package control

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register gif decoder
	"image/jpeg"
	"image/png"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register webp decoder
)

// maxVisionDim caps the longest image side sent to a model. OpenAI and Anthropic
// downscale to roughly this server-side anyway, so a larger upload only wastes
// request bytes and image tokens without adding fidelity.
const maxVisionDim = 1568

// maxDecodePixels guards against decompression-bomb attachments: a tiny file can
// declare enormous dimensions. Beyond this we skip decoding and send as-is (still
// bounded by the 64 MB file cap).
const maxDecodePixels = 50_000_000

const minInboxVisionDim = 512

// compressForVision downscales an oversized image to maxVisionDim and re-encodes
// it — PNG/GIF stay lossless (screenshots, text, transparency), JPEG/WebP go to
// JPEG. Best-effort: an undecodable format, a decode/encode failure, or an image
// already within budget returns the original bytes and mime unchanged.
func compressForVision(raw []byte, mime string) ([]byte, string) {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return raw, mime // bmp/tiff/svg: no decoder wired, send original
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width*cfg.Height > maxDecodePixels {
		return raw, mime
	}
	if cfg.Width <= maxVisionDim && cfg.Height <= maxVisionDim {
		return raw, mime // within budget — no point re-encoding
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw, mime
	}
	w, h := scaledDims(cfg.Width, cfg.Height, maxVisionDim)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if mime == "image/png" || mime == "image/gif" {
		if err := png.Encode(&buf, dst); err != nil {
			return raw, mime
		}
		return buf.Bytes(), "image/png"
	}
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return raw, mime
	}
	return buf.Bytes(), "image/jpeg"
}

// scaledDims returns dimensions with the longest side clamped to m, preserving
// aspect ratio (each side at least 1px).
func scaledDims(w, h, m int) (int, int) {
	if w >= h {
		nh := max(h*m/w, 1)
		return m, nh
	}
	nw := max(w*m/h, 1)
	return nw, m
}

// compressVisionDataURLToSize creates a smaller queue-only snapshot. It keeps
// pixel dimensions ahead of JPEG quality because OCR benefits more from glyph
// resolution than from near-lossless color reproduction.
func compressVisionDataURLToSize(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	comma := strings.IndexByte(value, ',')
	if comma <= len("data:image/") || !strings.HasPrefix(value, "data:image/") || !strings.HasSuffix(value[:comma], ";base64") {
		return value, false
	}
	raw, err := base64.StdEncoding.DecodeString(value[comma+1:])
	if err != nil {
		return value, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxDecodePixels {
		return value, false
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return value, false
	}

	longSide := max(cfg.Width, cfg.Height)
	longSide = min(longSide, maxVisionDim)
	dims := []int{longSide}
	for dims[len(dims)-1] > minInboxVisionDim {
		next := max(dims[len(dims)-1]*4/5, minInboxVisionDim)
		if next == dims[len(dims)-1] {
			break
		}
		dims = append(dims, next)
	}
	qualities := []int{92, 86, 80, 74, 68, 60}
	smallest := value
	for _, maxDim := range dims {
		w, h := cfg.Width, cfg.Height
		if max(w, h) > maxDim {
			w, h = scaledDims(w, h, maxDim)
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
		for _, quality := range qualities {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
				continue
			}
			candidate := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
			if len(candidate) < len(smallest) {
				smallest = candidate
			}
			if len(candidate) <= maxBytes {
				return candidate, true
			}
		}
	}
	return smallest, smallest != value
}
