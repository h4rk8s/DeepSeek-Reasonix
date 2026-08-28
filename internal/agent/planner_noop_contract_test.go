package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestCoordinatorDoesNotGuessNoOpFromGreetingPhrases(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: submitPlanChunk(`{"objective":"respond to the greeting","steps":[{"title":"没有具体任务；随时告诉我有什么需要我帮忙。"}]}`)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "executor final"},
		{Type: provider.ChunkDone},
	}}

	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, plannerRegistryWithSubmitPlan(), Options{}, executor, 0, event.Discard, nil)
	if err := coord.Run(withNoClosedLoop(context.Background()), "你好"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(exec.requests); got != 1 {
		t.Fatalf("executor requests = %d, want 1", got)
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "没有具体任务") {
		t.Fatalf("executor did not receive the submitted guidance plan: %q", got)
	}
}
