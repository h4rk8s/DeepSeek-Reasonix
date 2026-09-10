package control

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"reasonix/internal/sessioninbox"
)

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func makeNoisyTestPNG(t *testing.T, w, h int, seed uint32) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	state := seed
	for i := 0; i < len(img.Pix); i += 4 {
		state = state*1664525 + 1013904223
		img.Pix[i] = byte(state >> 24)
		img.Pix[i+1] = byte(state >> 16)
		img.Pix[i+2] = byte(state >> 8)
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode noisy png: %v", err)
	}
	return buf.Bytes()
}

func imageDataURL(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

func TestCompressForVisionDownscalesOversizedPNG(t *testing.T) {
	raw := makeTestPNG(t, 3000, 1500)
	out, mime := compressForVision(raw, "image/png")
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode out: %v", err)
	}
	// Pixel count is what governs vision token cost; assert the reduction there
	// (byte size isn't a robust invariant for synthetic, highly-compressible input).
	if cfg.Width != maxVisionDim || cfg.Height != 1500*maxVisionDim/3000 {
		t.Errorf("dims = %dx%d, want %dx%d", cfg.Width, cfg.Height, maxVisionDim, 1500*maxVisionDim/3000)
	}
	if cfg.Width*cfg.Height >= 3000*1500 {
		t.Errorf("pixel count %d not reduced from %d", cfg.Width*cfg.Height, 3000*1500)
	}
}

func TestCompressForVisionKeepsSmallImageVerbatim(t *testing.T) {
	raw := makeTestPNG(t, 100, 80)
	out, mime := compressForVision(raw, "image/png")
	if mime != "image/png" || !bytes.Equal(out, raw) {
		t.Errorf("an in-budget image must pass through unchanged (got %d bytes, mime %q)", len(out), mime)
	}
}

func TestCompressForVisionJPEGStaysJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2400, 1200)), nil); err != nil {
		t.Fatal(err)
	}
	out, mime := compressForVision(buf.Bytes(), "image/jpeg")
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", mime)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Width != maxVisionDim {
		t.Errorf("width = %d, want %d", cfg.Width, maxVisionDim)
	}
}

func TestCompressForVisionPassesThroughUndecodable(t *testing.T) {
	raw := []byte("<svg xmlns='...'></svg>")
	out, mime := compressForVision(raw, "image/svg+xml")
	if mime != "image/svg+xml" || !bytes.Equal(out, raw) {
		t.Error("an undecodable mime must pass through unchanged")
	}
}

func TestFitInboxEnvelopeImagesKeepsInBudgetImageVerbatim(t *testing.T) {
	original := imageDataURL("image/png", makeTestPNG(t, 100, 80))
	env := sessioninbox.PromptEnvelope{SubmitText: "inspect", FrozenImages: []string{original}}
	if err := fitInboxEnvelopeImages(&env, sessioninbox.DefaultMaxItemBytes); err != nil {
		t.Fatal(err)
	}
	if got := env.FrozenImages[0]; got != original {
		t.Fatal("an in-budget queue image was rewritten")
	}
}

func TestFitInboxEnvelopeImagesFailureIsActionable(t *testing.T) {
	unsupported := imageDataURL("image/svg+xml", bytes.Repeat([]byte("x"), 2048))
	env := sessioninbox.PromptEnvelope{SubmitText: "inspect", FrozenImages: []string{unsupported}}
	err := fitInboxEnvelopeImages(&env, 1024)
	if !errors.Is(err, sessioninbox.ErrItemTooLarge) {
		t.Fatalf("error = %v, want ErrItemTooLarge", err)
	}
	for _, want := range []string{"automatic image compression", "1.0 KiB", "wait until idle"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}
