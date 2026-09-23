package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRunRetriesPostToolReasoningOnlyStopForVisibleFinal(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("call-1", "echo", `{"text":"input"}`),
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "tool_calls", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkReasoning, Text: "The inspection is complete."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "visible findings"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
	}}
	reg := tool.NewRegistry()
	reg.Add(echoTool{})
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "inspect the input"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want tool round, reasoning-only stop, and visible-final retry", prov.call)
	}
	if got := lastUser(prov.requests[2]); !strings.Contains(got, "visible answer") {
		t.Fatalf("retry prompt = %q, want visible-answer nudge", got)
	}
	if got := lastAssistantContent(a.sess.conversation); got != "visible findings" {
		t.Fatalf("last assistant content = %q, want visible findings", got)
	}
}
