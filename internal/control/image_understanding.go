package control

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/nilutil"
)

// ImageUnderstandingRef is one user-referenced image prepared for an optional
// sidecar. DataURL is for provider-native vision; Path/Source/SHA256 let local
// OCR commands emit portable, auditable context without inlining image bytes.
type ImageUnderstandingRef struct {
	Source  string
	Path    string
	DataURL string
	SHA256  string
}

// ImageUnderstanding describes attached user images, producing compact text
// that can be injected into a text-only main turn.
type ImageUnderstanding interface {
	DescribeImages(ctx context.Context, userInput string, images []ImageUnderstandingRef) (string, error)
}

func (c *Controller) tryLocalImageUnderstanding(ctx context.Context, input string, sourceInputs ...string) (string, bool, error) {
	if nilutil.IsNil(c.imageUnderstanding) || c.imageInputEnabled() {
		return input, false, nil
	}
	source := input
	for _, candidate := range sourceInputs {
		if strings.TrimSpace(candidate) != "" {
			source = candidate
			break
		}
	}
	images := c.inputImageRefs(source)
	if len(images) == 0 {
		return input, false, nil
	}
	started := time.Now()
	desc, err := c.imageUnderstanding.DescribeImages(ctx, source, images)
	if err != nil {
		return input, false, err
	}
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return input, false, fmt.Errorf("local image understanding returned no usable context")
	}
	c.emitImageUnderstandingNotice(len(images), desc, time.Since(started))
	return "Image understanding context:\n\n" + desc + "\n\n" + input, true, nil
}

func normalizeImageUnderstandingLog(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off", "none", "false", "0", "disabled":
		return "off"
	case "detail", "details", "verbose", "full":
		return "detail"
	default:
		return "summary"
	}
}

func (c *Controller) emitImageUnderstandingNotice(count int, desc string, elapsed time.Duration) {
	mode := normalizeImageUnderstandingLog(c.imageUnderstandingLog)
	if mode == "off" {
		return
	}
	label := "image"
	if count != 1 {
		label = "images"
	}
	parts := []string{fmt.Sprintf("%d %s", count, label), "OCR + UI state"}
	if elapsedText := formatImageUnderstandingElapsed(elapsed); elapsedText != "" {
		parts = append(parts, elapsedText)
	}
	e := event.Event{
		Kind:   event.Notice,
		Level:  event.LevelInfo,
		Source: event.UsageSourceVision,
		Text:   "image understood: " + strings.Join(parts, " · "),
		Detail: desc,
	}
	c.sink.Emit(e)
}

func formatImageUnderstandingElapsed(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Second {
		ms := int(d.Round(time.Millisecond) / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
		return fmt.Sprintf("%dms", ms)
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

func formatImageUnderstandingBlock(source, sha, visibleText, uiState, errors, layout, confidence string) string {
	if strings.TrimSpace(confidence) == "" {
		confidence = "medium"
	}
	var b strings.Builder
	b.WriteString(imageUnderstandingOpenTag(source, sha))
	b.WriteString("\n")
	writeImageUnderstandingField(&b, "visible_text", visibleText)
	writeImageUnderstandingField(&b, "ui_state", uiState)
	writeImageUnderstandingField(&b, "errors", errors)
	writeImageUnderstandingField(&b, "layout", layout)
	writeImageUnderstandingField(&b, "confidence", confidence)
	b.WriteString("\n</image-understanding>")
	return b.String()
}

func imageUnderstandingOpenTag(source, sha string) string {
	var b strings.Builder
	b.WriteString(`<image-understanding source="`)
	b.WriteString(html.EscapeString(strings.TrimSpace(source)))
	b.WriteString(`"`)
	if strings.TrimSpace(sha) != "" {
		b.WriteString(` sha256="`)
		b.WriteString(html.EscapeString(strings.TrimSpace(sha)))
		b.WriteString(`"`)
	}
	b.WriteString(">")
	return b.String()
}

func writeImageUnderstandingField(b *strings.Builder, name, value string) {
	value = strings.TrimSpace(value)
	b.WriteString(name)
	b.WriteString(":")
	if value != "" {
		b.WriteString(" ")
		if strings.Contains(value, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(value)
	}
	b.WriteString("\n")
}
