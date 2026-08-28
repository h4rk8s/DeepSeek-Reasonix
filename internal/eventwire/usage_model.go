package eventwire

import (
	"strings"

	"reasonix/internal/event"
)

func usageModelRef(e event.Event) string {
	if modelRef := strings.TrimSpace(e.ModelRef); modelRef != "" {
		return modelRef
	}
	return strings.TrimSpace(e.UsageModel)
}
