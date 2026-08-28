package control

import (
	"context"
	"html"
	"strings"
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
