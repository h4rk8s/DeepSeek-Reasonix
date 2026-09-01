package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type countedPlacementTool struct {
	name  string
	body  string
	calls int
}

func (t *countedPlacementTool) Name() string      { return t.name }
func (*countedPlacementTool) Description() string { return "return a fixed test payload" }
func (*countedPlacementTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (*countedPlacementTool) ReadOnly() bool { return true }
func (t *countedPlacementTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.calls++
	return t.body, nil
}

type recordingPlacementProvider struct {
	sharedWindowTestProvider
	requests []provider.Request
}

func (p *recordingPlacementProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	return p.sharedWindowTestProvider.Stream(ctx, req)
}

func placementAgent(t *testing.T, payloadBytes int) (*Agent, *recordingPlacementProvider, *countedPlacementTool, *countedPlacementTool) {
	t.Helper()
	prov := &recordingPlacementProvider{sharedWindowTestProvider: sharedWindowTestProvider{budget: 128 * 1024, shared: true}}
	sess := foldableSessionOverForce(6)
	a := agentOverForceWindow(t, prov, sess, 50_000)
	first := &countedPlacementTool{name: "placement_first", body: "PLACEMENT_FIRST_MARKER " + strings.Repeat("a", payloadBytes)}
	second := &countedPlacementTool{name: "placement_second", body: "PLACEMENT_SECOND_MARKER " + strings.Repeat("b", payloadBytes)}
	reg := tool.NewRegistry()
	reg.Add(first)
	reg.Add(second)
	a.SetTools(reg)
	return a, prov, first, second
}

func placementTurn() *turnRuntime {
	return &turnRuntime{seenTodoProgress: make(map[string]struct{})}
}

func fillPlacementView(t *testing.T, a *Agent, remaining int) {
	t.Helper()
	for a.compactTrigger()-a.estimatedVisibleRequestTokens(a.modelVisibleMessages()) > remaining {
		a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("context ", 500)})
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
}

func TestToolPlacementMaintainsOnlyAfterCompleteBatch(t *testing.T) {
	a, prov, first, second := placementAgent(t, 8_000)
	fillPlacementView(t, a, 4_000)
	calls := []provider.ToolCall{
		{ID: "placement-1", Name: first.Name(), Arguments: `{}`},
		{ID: "placement-2", Name: second.Name(), Arguments: `{}`},
	}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})

	cont, err := a.handleToolRound(context.Background(), placementTurn(), 0, "", "", calls, nil)
	if err != nil || !cont {
		t.Fatalf("handleToolRound = (%v, %v), want continuation", cont, err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("tool executions = (%d, %d), want exactly once each", first.calls, second.calls)
	}
	if len(prov.requests) == 0 || a.currentProjectionVersion() == 0 {
		t.Fatalf("post-tool pressure did not install maintenance: requests=%d version=%d", len(prov.requests), a.currentProjectionVersion())
	}
	for _, req := range prov.requests {
		var observed strings.Builder
		for _, msg := range req.Messages {
			observed.WriteString(msg.Content)
		}
		text := observed.String()
		if strings.Contains(text, "PLACEMENT_FIRST_MARKER") && strings.Contains(text, "PLACEMENT_SECOND_MARKER") {
			return
		}
	}
	t.Fatal("maintenance never observed both completed tool results")
}

func TestToolPlacementBelowSoftLineIsProviderNoop(t *testing.T) {
	a, prov, first, _ := placementAgent(t, 16)
	calls := []provider.ToolCall{{ID: "placement-small", Name: first.Name(), Arguments: `{}`}}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})

	cont, err := a.handleToolRound(context.Background(), placementTurn(), 0, "", "", calls, nil)
	if err != nil || !cont {
		t.Fatalf("handleToolRound = (%v, %v), want continuation", cont, err)
	}
	if first.calls != 1 {
		t.Fatalf("tool executions = %d, want 1", first.calls)
	}
	if len(prov.requests) != 0 || a.currentProjectionVersion() != 0 {
		t.Fatalf("roomy placement changed provider state: requests=%d version=%d", len(prov.requests), a.currentProjectionVersion())
	}
}

func TestToolPlacementOverHardCeilingRecoversOrFailsClosed(t *testing.T) {
	prov := &failingSummaryProvider{}
	a := agentOverForceWindow(t, prov, foldableSessionOverForce(6), 50_000)
	testTool := &countedPlacementTool{
		name: "placement_hard_limit",
		body: "PLACEMENT_HARD_LIMIT_MARKER " + strings.Repeat("placement payload segment ", 2_500),
	}
	reg := tool.NewRegistry()
	reg.Add(testTool)
	a.SetTools(reg)
	for a.hardInputCeiling()-a.estimatedVisibleRequestTokens(a.modelVisibleMessages()) > 100 {
		a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("hard pressure context ", 500)})
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	calls := []provider.ToolCall{{ID: "placement-hard", Name: testTool.Name(), Arguments: `{}`}}
	a.sess.conversation.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})

	cont, err := a.handleToolRound(context.Background(), placementTurn(), 0, "", "", calls, nil)
	if err != nil && (cont || !errors.Is(err, ErrCompactionRequired)) {
		t.Fatalf("handleToolRound = (%v, %v), want a closed compaction failure", cont, err)
	}
	if err == nil {
		if !cont {
			t.Fatal("successful hard-ceiling recovery did not continue")
		}
		if current, hard := a.contextManager().currentPrepared().InputTokens, a.hardInputCeiling(); current >= hard {
			t.Fatalf("successful recovery left context over the hard ceiling: %d >= %d", current, hard)
		}
	}
	if testTool.calls != 1 {
		t.Fatalf("tool executions = %d, want 1", testTool.calls)
	}
	var paired bool
	for _, msg := range a.sess.conversation.Snapshot() {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "placement-hard" && strings.Contains(msg.Content, "PLACEMENT_HARD_LIMIT_MARKER") {
			paired = true
		}
	}
	if !paired {
		t.Fatal("hard-ceiling failure lost the completed tool result")
	}
}
