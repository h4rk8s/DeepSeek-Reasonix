package transcript

import (
	"strings"

	"reasonix/internal/event"
)

// Kind is the stable semantic identity shared by live and replay frontends.
type Kind string

const (
	KindUnknown            Kind = "unknown"
	KindTurnBoundary       Kind = "turn_boundary"
	KindUserPrompt         Kind = "user_prompt"
	KindPlannerPhase       Kind = "planner_phase"
	KindThinkingDisclosure Kind = "thinking_disclosure"
	KindToolActivity       Kind = "tool_activity"
	KindImageDisclosure    Kind = "image_disclosure"
	KindAssistant          Kind = "assistant"
	KindRecap              Kind = "recap"
	KindTurnMetrics        Kind = "turn_metrics"
	KindRecoveryNotice     Kind = "recovery_notice"
	KindNotice             Kind = "notice"
	KindCompaction         Kind = "compaction"
	KindExtension          Kind = "extension"
)

// Entry keeps the semantic name introduced for cross-frontend projections
// while reusing Message, the upstream session-v3 display contract.
type Entry = Message

func KindForHistory(role, code string) Kind {
	switch strings.TrimSpace(role) {
	case "user":
		return KindUserPrompt
	case "assistant":
		return KindAssistant
	case "tool":
		return KindToolActivity
	case "phase":
		return KindPlannerPhase
	case "compaction":
		return KindCompaction
	case "notice", "protocol_recovery", "final_readiness":
		if recoveryNoticeCode(code) || strings.TrimSpace(role) != "notice" {
			return KindRecoveryNotice
		}
		return KindNotice
	default:
		return KindUnknown
	}
}

func KindForEvent(e event.Event) Kind {
	switch e.Kind {
	case event.TurnStarted, event.TurnDone, event.TurnStatusChanged:
		return KindTurnBoundary
	case event.Reasoning:
		return KindThinkingDisclosure
	case event.Text, event.Message:
		return KindAssistant
	case event.ToolDispatch, event.ToolProgress, event.ToolResult, event.ToolResultPreview:
		return KindToolActivity
	case event.Usage:
		return KindTurnMetrics
	case event.Notice:
		if e.Source == event.UsageSourceVision || strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.Text)), "image understood:") {
			return KindImageDisclosure
		}
		if recoveryNoticeCode(e.Code) {
			return KindRecoveryNotice
		}
		return KindNotice
	case event.Phase:
		return KindPlannerPhase
	case event.CompactionStarted, event.CompactionDone, event.ContextMaintenanceEvent:
		return KindCompaction
	case event.ExtensionSurface, event.ExtensionStatus:
		return KindExtension
	default:
		return KindUnknown
	}
}

func Normalize(entry Entry) Entry {
	if entry.Kind == "" || entry.Kind == KindUnknown {
		entry.Kind = KindForHistory(entry.Role, entry.Code)
	}
	return entry
}

func NormalizeAll(entries []Entry) []Entry {
	for i := range entries {
		entries[i] = Normalize(entries[i])
	}
	return entries
}

func recoveryNoticeCode(code string) bool {
	switch strings.TrimSpace(code) {
	case event.NoticeCodeCancelledTurn, event.NoticeCodeFinalReadiness, "protocol_recovery":
		return true
	default:
		return false
	}
}
