package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type capabilityShapeTool struct {
	name   string
	schema json.RawMessage
}

func (t capabilityShapeTool) Name() string            { return t.name }
func (t capabilityShapeTool) Description() string     { return t.name + " capability" }
func (t capabilityShapeTool) Schema() json.RawMessage { return t.schema }
func (t capabilityShapeTool) ReadOnly() bool          { return false }
func (t capabilityShapeTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.name + " done", nil
}

func TestUseCapabilityCanonicalizesPortableMCPReference(t *testing.T) {
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, tool.NewRegistry(), nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "mcp-tool:exa/web_search_exa", Kind: capability.KindMCPTool,
			Name: "exa/web_search_exa", Source: "exa", ToolName: "mcp__exa__web_search_exa",
		}}}
	})
	resolved, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{
		"action":"call",
		"capability_id":"mcp/exa/web_search_exa",
		"arguments":{"query":"reasonix"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CapabilityID != "mcp-tool:exa/web_search_exa" {
		t.Fatalf("capability id = %q", resolved.CapabilityID)
	}
	if !resolved.Unavailable {
		t.Fatal("fixture has no MCP runtime; canonical resolution should remain fail-closed")
	}
}

func TestUseCapabilityRejectsAmbiguousPortableReferenceWithCandidates(t *testing.T) {
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, tool.NewRegistry(), nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{
			{ID: "mcp-tool:browser/search", Kind: capability.KindMCPTool, Name: "browser/search", Source: "browser"},
			{ID: "mcp-tool:exa/search", Kind: capability.KindMCPTool, Name: "exa/search", Source: "exa"},
		}}
	})
	_, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{"action":"call","capability_id":"search","arguments":{}}`))
	if err == nil {
		t.Fatal("ambiguous reference resolved")
	}
	for _, want := range []string{"ambiguous_capability_reference", "mcp-tool:browser/search", "mcp-tool:exa/search"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestUseCapabilityMissingIDReturnsOneExactRetryContract(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(capabilityShapeTool{name: "remember", schema: json.RawMessage(`{
		"type":"object",
		"properties":{"description":{"type":"string"},"body":{"type":"string"}},
		"required":["description","body"]
	}`)})
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	})
	_, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{
		"action":"call",
		"arguments":{"description":"cache rule","body":"keep the prefix stable"}
	}`))
	if err == nil {
		t.Fatal("missing capability id resolved without an explicit reference")
	}
	for _, want := range []string{
		"missing_capability_id",
		"tool:remember",
		`"capability_id":"tool:remember"`,
		`"description":"cache rule"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestUseCapabilityUniqueQueryRepairsMissingID(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(capabilityShapeTool{name: "remember", schema: json.RawMessage(`{"type":"object"}`)})
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "tool:remember", Kind: capability.KindTool, Name: "remember", ToolName: "remember", Status: capability.StatusReady,
		}}}
	})
	resolved, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{
		"action":"call",
		"query":"remember",
		"arguments":{}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CapabilityID != "tool:remember" || resolved.TargetName != "remember" {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestUseCapabilityUnknownQueryReportsNoCandidate(t *testing.T) {
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, tool.NewRegistry(), nil, nil, func() capability.Catalog {
		return capability.Catalog{}
	})
	_, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{
		"action":"call","query":"capability-that-does-not-exist","arguments":{}
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown_capability_query") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "candidates=") {
		t.Fatalf("zero-result repair invented candidates: %v", err)
	}
}

func TestUseCapabilitySemanticNamespaceAliasNormalizesToCatalogID(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(capabilityShapeTool{name: "remember", schema: json.RawMessage(`{"type":"object"}`)})
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
	})
	resolved, err := proxy.ResolveCall(t.Context(), json.RawMessage(`{
		"action":"call","capability_id":"memory:remember","arguments":{}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CapabilityID != "tool:remember" || resolved.TargetName != "remember" {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestUseCapabilityResolutionUsesSharedRepeatReminder(t *testing.T) {
	reg := tool.NewRegistry()
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{}
	})
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	call := provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","arguments":{}}`}
	var last string
	for i := range 3 {
		last = a.executeBatch(t.Context(), &a.turn, []provider.ToolCall{call}).results[0]
		if i < 2 && (!strings.Contains(last, "missing_capability_id") || !strings.Contains(last, "not executed") || strings.Contains(last, "[repeat reminder]")) {
			t.Fatalf("recoverable outcome %d = %q", i+1, last)
		}
	}
	if !strings.Contains(last, "[repeat reminder]") || !strings.Contains(last, "missing_capability_id") {
		t.Fatalf("shared repeat reminder outcome = %q", last)
	}
}

func TestUseCapabilitySuccessResetsSharedRepeatReminder(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "remember", readOnly: false})
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "tool:remember", Kind: capability.KindTool, Name: "remember", ToolName: "remember", Status: capability.StatusReady,
		}}}
	})
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	bad := provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","arguments":{}}`}
	good := provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","capability_id":"remember","arguments":{}}`}
	for range 2 {
		out := a.executeBatch(t.Context(), &a.turn, []provider.ToolCall{bad}).results[0]
		if strings.Contains(out, "[repeat reminder]") {
			t.Fatalf("recoverable call was reminded early: %q", out)
		}
	}
	if out := a.executeBatch(t.Context(), &a.turn, []provider.ToolCall{good}).results[0]; stripReceiptCitation(out) != "remember done" {
		t.Fatalf("successful repair = %q", out)
	}
	for range 2 {
		out := a.executeBatch(t.Context(), &a.turn, []provider.ToolCall{bad}).results[0]
		if strings.Contains(out, "[repeat reminder]") {
			t.Fatalf("new failure streak inherited stale state: %q", out)
		}
	}
}

func TestCapabilityGatewayReceiptsBindPermissionAndResultToCanonicalTarget(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "remember", readOnly: false})
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "tool:remember", Kind: capability.KindTool, Name: "remember", ToolName: "remember", Status: capability.StatusReady,
		}}}
	})
	reg.Add(proxy)
	gate := &stubGate{deny: map[string]bool{}}
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(nil, reg, NewSession("sys"), Options{Gate: gate}, sink)
	call := provider.ToolCall{ID: "cap-1", Name: "use_capability", Arguments: `{
		"action":"call","capability_id":"remember","arguments":{}
	}`}
	batch := a.executeBatch(t.Context(), &a.turn, []provider.ToolCall{call})
	if batch.err != nil || len(batch.results) != 1 || stripReceiptCitation(batch.results[0]) != "remember done" {
		t.Fatalf("batch = %+v", batch)
	}
	if len(gate.checked) != 1 || gate.checked[0] != "remember" {
		t.Fatalf("permission targets = %v", gate.checked)
	}
	var resolvedDispatch, result *event.Tool
	for i := range events {
		e := &events[i]
		if e.Kind == event.ToolDispatch && e.Tool.Refreshed {
			resolvedDispatch = &e.Tool
		}
		if e.Kind == event.ToolResult {
			result = &e.Tool
		}
	}
	for label, receipt := range map[string]*event.Tool{"resolved dispatch": resolvedDispatch, "result": result} {
		if receipt == nil || receipt.ResolvedName != "remember" || receipt.CapabilityID != "tool:remember" {
			t.Fatalf("%s receipt = %+v", label, receipt)
		}
	}
}
