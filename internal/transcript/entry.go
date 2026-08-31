package transcript

import (
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
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

// Entry is the additive history wire contract shared by Desktop and Serve.
// Role remains for old clients; Kind is the preferred semantic discriminator.
type Entry struct {
	CompletionReceipt  *eventwire.CompletionReceipt     `json:"completionReceipt,omitempty"`
	CompletionSummary  *eventwire.CompletionSummary     `json:"completionSummary,omitempty"`
	TurnID             string                           `json:"turnId,omitempty"`
	Kind               Kind                             `json:"kind,omitempty"`
	Role               string                           `json:"role"`
	Content            string                           `json:"content"`
	Detail             string                           `json:"detail,omitempty"`
	Code               string                           `json:"code,omitempty"`
	SubmitText         string                           `json:"submitText,omitempty"`
	CheckpointTurn     *int                             `json:"checkpointTurn,omitempty"`
	CreatedAt          int64                            `json:"createdAt,omitempty"`
	Reasoning          string                           `json:"reasoning,omitempty"`
	MemoryCitations    []provider.MemoryCitation        `json:"memoryCitations,omitempty"`
	WorkDurationMs     int64                            `json:"workDurationMs,omitempty"`
	Level              string                           `json:"level,omitempty"`
	ToolCalls          []ToolCall                       `json:"toolCalls,omitempty"`
	ToolCallID         string                           `json:"toolCallId,omitempty"`
	ToolName           string                           `json:"toolName,omitempty"`
	ToolResultArchived bool                             `json:"toolResultArchived,omitempty"`
	ToolResultError    string                           `json:"toolResultError,omitempty"`
	Execution          *provider.ToolExecution          `json:"execution,omitempty"`
	Pending            bool                             `json:"pending,omitempty"`
	Trigger            string                           `json:"trigger,omitempty"`
	Messages           int                              `json:"messages,omitempty"`
	Summary            string                           `json:"summary,omitempty"`
	Archive            string                           `json:"archive,omitempty"`
	DecisionReceipt    *provider.DecisionReceipt        `json:"decisionReceipt,omitempty"`
	Readiness          *event.FinalReadiness            `json:"readiness,omitempty"`
	ReadPause          *provider.ReadPause              `json:"readPause,omitempty"`
	ReadCompletion     *provider.ReadCompletion         `json:"readCompletion,omitempty"`
	ProtocolRecovery   *provider.ProtocolRecoveryAction `json:"protocolRecovery,omitempty"`
	Diagnostic         *provider.FailureDiagnostic      `json:"diagnostic,omitempty"`
	Missing            []string                         `json:"missing,omitempty"`
	ServerSearch       []provider.ServerSearchCall      `json:"serverSearch,omitempty"`
}

type ToolCall struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Arguments         string `json:"arguments"`
	ResolvedName      string `json:"resolvedName,omitempty"`
	CapabilityID      string `json:"capabilityId,omitempty"`
	ResolvedReadOnly  *bool  `json:"resolvedReadOnly,omitempty"`
	Subject           string `json:"subject,omitempty"`
	Summary           string `json:"summary,omitempty"`
	Diff              string `json:"diff,omitempty"`
	Added             int    `json:"added,omitempty"`
	Removed           int    `json:"removed,omitempty"`
	ArgumentsArchived bool   `json:"argumentsArchived,omitempty"`
}

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
	case "notice":
		if recoveryNoticeCode(code) {
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
