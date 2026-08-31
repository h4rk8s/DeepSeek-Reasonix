package transcript

import (
	"encoding/json"
	"testing"

	"reasonix/internal/event"
)

func TestLiveAndHistoryKindsShareSemanticIdentity(t *testing.T) {
	tests := []struct {
		name string
		live event.Event
		role string
		code string
		want Kind
	}{
		{name: "assistant", live: event.Event{Kind: event.Message}, role: "assistant", want: KindAssistant},
		{name: "tool", live: event.Event{Kind: event.ToolResult}, role: "tool", want: KindToolActivity},
		{name: "phase", live: event.Event{Kind: event.Phase}, role: "phase", want: KindPlannerPhase},
		{name: "recovery", live: event.Event{Kind: event.Notice, Code: event.NoticeCodeCancelledTurn}, role: "notice", code: event.NoticeCodeCancelledTurn, want: KindRecoveryNotice},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KindForEvent(tt.live); got != tt.want {
				t.Fatalf("live kind = %q, want %q", got, tt.want)
			}
			if got := KindForHistory(tt.role, tt.code); got != tt.want {
				t.Fatalf("history kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEntryJSONContractIsAdditiveForLegacyClients(t *testing.T) {
	var legacy Entry
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"done"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy = Normalize(legacy)
	if legacy.Kind != KindAssistant {
		t.Fatalf("legacy kind = %q, want %q", legacy.Kind, KindAssistant)
	}

	wire, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["role"] != "assistant" || fields["content"] != "done" || fields["kind"] != string(KindAssistant) {
		t.Fatalf("additive wire contract = %s", wire)
	}
}

func TestVisionNoticeIsImageDisclosure(t *testing.T) {
	for _, e := range []event.Event{
		{Kind: event.Notice, Source: event.UsageSourceVision},
		{Kind: event.Notice, Text: "Image understood: 2 images"},
	} {
		if got := KindForEvent(e); got != KindImageDisclosure {
			t.Fatalf("vision notice kind = %q", got)
		}
	}
}
