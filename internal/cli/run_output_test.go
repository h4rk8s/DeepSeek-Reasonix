package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func quotedRunUsageEvent(model, source string, usage *provider.Usage, pricing provider.Pricing) event.Event {
	e := event.Event{Kind: event.Usage, ModelRef: model, UsageSource: source, Usage: usage, Pricing: &pricing}
	e.CostQuote = event.EnsureCostQuote(e, nil)
	e.Pricing = nil
	return e
}

func TestRunOutputTextPrintsOnlyFinalMessage(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputText)
	sink.Emit(event.Event{Kind: event.Text, Text: "streamed "})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Output: "noise"}})
	sink.Emit(event.Event{Kind: event.Message, Text: "final answer"})
	if err := sink.Finalize("session", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "final answer\n" {
		t.Fatalf("text output = %q", got)
	}
}

func TestRunOutputJSONResult(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.Message, Text: "done"})
	sink.Emit(event.Event{Kind: event.Usage, ModelRef: "provider/deepseek", Usage: &provider.Usage{
		PromptTokens: 12, CompletionTokens: 3, CacheHitTokens: 8, CacheMissTokens: 4, Estimated: true,
	}})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, out.String())
	}
	if result.SchemaVersion != 2 || result.Type != "result" || result.Subtype != "success" || result.IsError || result.Result != "done" || result.SessionID != "abc" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 || result.Usage.CacheReadInputTokens != 8 || result.Usage.CacheCreationInputTokens != 4 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if !result.Usage.Estimated {
		t.Fatalf("usage lost estimated marker: %+v", result.Usage)
	}
}

func TestRunOutputJSONIncludesCurrencyAwareCostFields(t *testing.T) {
	for _, tt := range []struct {
		name     string
		currency string
		wantCode string
	}{
		{name: "USD", currency: "$", wantCode: "USD"},
		{name: "CNY", currency: "¥", wantCode: "CNY"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			sink := newRunOutputSink(&out, runOutputJSON)
			sink.Emit(quotedRunUsageEvent("provider/model", event.UsageSourceExecutor,
				&provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 500_000},
				provider.Pricing{Input: 1, Output: 2, Currency: tt.currency}))
			if err := sink.Finalize("abc", time.Now(), nil); err != nil {
				t.Fatal(err)
			}
			var result runResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.TotalCost == nil || *result.TotalCost != 2 || result.Currency != tt.wantCode {
				t.Fatalf("currency-aware result = %+v", result)
			}
			if tt.wantCode == "USD" {
				if result.TotalCostUSD == nil || *result.TotalCostUSD != *result.TotalCost {
					t.Fatalf("USD compatibility total = %+v", result)
				}
			} else if result.TotalCostUSD != nil {
				t.Fatalf("non-USD result must omit normalized USD total: %+v", result)
			}
		})
	}
}

func TestRunOutputJSONTotalsMoreThanAuditLimit(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	for range 65 {
		sink.Emit(quotedRunUsageEvent("provider/model", event.UsageSourceExecutor,
			&provider.Usage{PromptTokens: 1_000_000}, provider.Pricing{Input: 1, Currency: "USD"}))
	}
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.CostComplete || result.TotalCost == nil || *result.TotalCost != 65 || result.Currency != "USD" {
		t.Fatalf("65-event total was truncated: %+v", result)
	}
	if result.CostQuote == nil || result.CostQuote.Selected == nil || result.CostQuote.Selected.Amount != "65" {
		t.Fatalf("65-event aggregate quote = %+v", result.CostQuote)
	}
}

func TestRunOutputJSONRejectsMixedPricingCurrencies(t *testing.T) {
	// Mixed originals no longer error: they emit original_costs + cost_complete=false
	// when a shared display valuation is unavailable (no FX table in unit test).
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	for _, currency := range []string{"$", "¥"} {
		sink.Emit(quotedRunUsageEvent("provider/model", event.UsageSourceExecutor,
			&provider.Usage{PromptTokens: 1_000_000}, provider.Pricing{Input: 1, Currency: currency}))
	}
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatalf("Finalize mixed currencies: %v", err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.CostComplete || result.DisplayComplete || result.DisplayStatus != "bucketed" {
		t.Fatalf("expected complete cost facts but bucketed display, got %+v", result)
	}
	if len(result.OriginalCosts) < 2 {
		t.Fatalf("expected per-currency original_costs, got %+v", result.OriginalCosts)
	}
}

func TestRunOutputSessionIDPreservesExistingFormats(t *testing.T) {
	const raw = "20260723-120000.000000000-model"
	identityKey := bytes.Repeat([]byte{0x41}, machineIdentityKeyBytes)
	for _, format := range []runOutputFormat{runOutputText, runOutputJSON, runOutputStreamJSON} {
		if got := runOutputSessionID(format, raw, nil); got != raw {
			t.Fatalf("format %q session id = %q, want raw id %q", format, got, raw)
		}
	}
	if got := runOutputSessionID(runOutputEventsJSONL, raw, identityKey); got != machineSessionIDWithKey(raw, identityKey) {
		t.Fatalf("events-jsonl session id = %q, want machine id %q", got, machineSessionIDWithKey(raw, identityKey))
	}
}

func TestRunOutputJSONUnknownPricingFailsClosed(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.Message, Text: "done"})
	sink.Emit(event.Event{Kind: event.Usage, ModelRef: "provider/deepseek", UsageModel: "legacy/wrong", Usage: &provider.Usage{
		PromptTokens: 100, CompletionTokens: 20, CacheHitTokens: 80, CacheMissTokens: 20,
	}, Pricing: &provider.Pricing{Input: 999, Output: 999, Currency: "USD"}})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, out.String())
	}
	if _, ok := result["total_cost_usd"]; ok {
		t.Fatalf("unknown pricing must omit total_cost_usd: %s", out.String())
	}
	if _, ok := result["total_cost"]; ok {
		t.Fatalf("unknown pricing must omit total_cost: %s", out.String())
	}
	if result["schema_version"] != float64(2) || result["cost_is_partial"] != true {
		t.Fatalf("v2 fail-closed fields missing: %s", out.String())
	}
	if _, ok := result["total_cost_usd_ticks"]; ok {
		t.Fatalf("unknown pricing must omit total_cost_usd_ticks: %s", out.String())
	}
	modelUsage := result["modelUsage"].(map[string]any)
	if _, ok := modelUsage["executor:provider/deepseek"]; !ok {
		t.Fatalf("model attribution missing: %s", out.String())
	}
	if _, ok := modelUsage["executor:legacy/wrong"]; ok {
		t.Fatalf("legacy UsageModel remained a model truth: %s", out.String())
	}
}

func TestRunOutputJSONCompleteUSDIncludesExactTicks(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(quotedRunUsageEvent("provider/planner", event.UsageSourcePlanner,
		&provider.Usage{PromptTokens: 3, CompletionTokens: 1, CacheMissTokens: 3},
		provider.Pricing{Input: 0.1, Output: 0.2, Currency: "USD"}))
	sink.Emit(quotedRunUsageEvent("provider/executor", event.UsageSourceExecutor,
		&provider.Usage{PromptTokens: 7, CompletionTokens: 2, CacheHitTokens: 7},
		provider.Pricing{CacheHit: 0.01, Output: 0.2, Currency: "USD"}))
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.CostIsPartial || result.TotalCostUSDTicks == nil || *result.TotalCostUSDTicks != 9700 || result.TotalCostUSD == nil || *result.TotalCostUSD != 0.00000097 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunOutputJSONReportsOpenBackgroundSubagent(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{
		ID: "task-1", Kind: "task", Status: "running",
	}})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.UsageIsIncomplete || !result.CostIsPartial || result.TotalCostUSD != nil || result.OpenBackgroundSubagents != 1 || len(result.IncompleteReasons) != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunOutputJSONUsesOccurrenceTimeQuoteWithoutRepricing(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	money := billing.Money{Amount: "0.125", Currency: "USD"}
	sink.Emit(event.Event{
		Kind: event.Usage, ModelRef: "provider/model", Usage: &provider.Usage{PromptTokens: 1_000_000},
		Pricing: &provider.Pricing{Input: 999, Currency: "USD"},
		CostQuote: &billing.CostQuote{
			Original: money, Selected: &money, CostComplete: true, DisplayComplete: true, Complete: true,
			DisplayStatus: billing.DisplayStatusMatched, ModelRef: "legacy/wrong", UsageSource: "wrong",
		},
	})
	if err := sink.Finalize("abc", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TotalCostUSDTicks == nil || *result.TotalCostUSDTicks != 1_250_000_000 || result.TotalCostUSD == nil || *result.TotalCostUSD != 0.125 {
		t.Fatalf("occurrence-time quote was not authoritative: %+v", result)
	}
}

func TestRunOutputStreamJSONEndsWithErrorResult(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputStreamJSON)
	sink.Emit(event.Event{Kind: event.Text, Text: "partial"})
	runErr := errors.New("provider failed")
	if err := sink.Finalize("abc", time.Now(), runErr); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("stream lines = %d, want 2\n%s", len(lines), out.String())
	}
	var wire map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &wire); err != nil || wire["kind"] != "text" {
		t.Fatalf("wire event = %#v, err=%v", wire, err)
	}
	var result runResult
	if err := json.Unmarshal([]byte(lines[1]), &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Subtype != "error_during_execution" || result.Result != runErr.Error() {
		t.Fatalf("error result = %+v", result)
	}
}

func TestRunOutputEventsJSONLIsStructuredAndRedacted(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputEventsJSONL)
	sink.Emit(event.Event{Kind: event.Text, Text: "PRIVATE ANSWER"})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "PRIVATE TOOL ID", Name: "PRIVATE TOOL NAME", Args: `{"command":"PRIVATE COMMAND"}`, Output: "PRIVATE OUTPUT", Err: "PRIVATE ERROR",
	}})
	sink.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "PRIVATE TOOL ID", Name: "PRIVATE TOOL NAME"}})
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: 4, CompletionTokens: 2, Estimated: true}})
	if err := sink.Finalize("session-1", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("event lines = %d, output = %s", len(lines), out.String())
	}
	var toolAliases []struct {
		ToolID   string `json:"tool_id"`
		ToolName string `json:"tool_name"`
	}
	var sawEstimatedUsage bool
	for i, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if payload["schema_version"] != float64(machineSchemaVersion) || payload["sequence"] != float64(i+1) {
			t.Fatalf("line %d envelope = %#v", i, payload)
		}
		if payload["kind"] == "tool_result" || payload["kind"] == "tool_progress" {
			var aliases struct {
				ToolID   string `json:"tool_id"`
				ToolName string `json:"tool_name"`
			}
			if err := json.Unmarshal([]byte(line), &aliases); err != nil {
				t.Fatal(err)
			}
			toolAliases = append(toolAliases, aliases)
		}
		if payload["kind"] == "usage" {
			usage, ok := payload["usage"].(map[string]any)
			sawEstimatedUsage = ok && usage["estimated"] == true
		}
	}
	if strings.Contains(out.String(), "PRIVATE") || !strings.Contains(out.String(), `"kind":"run_done"`) {
		t.Fatalf("event stream was not redacted or terminated: %s", out.String())
	}
	if len(toolAliases) != 2 || toolAliases[0].ToolID != "tool_1" || toolAliases[0].ToolName != "tool_name_1" || toolAliases[1] != toolAliases[0] {
		t.Fatalf("tool aliases = %+v, want stable per-run opaque identities", toolAliases)
	}
	if !sawEstimatedUsage {
		t.Fatalf("event stream lost estimated usage marker: %s", out.String())
	}
}

func TestEventsJSONLHasOneCanonicalFlag(t *testing.T) {
	if _, err := parseRunOutputFormat("events-jsonl"); err == nil {
		t.Fatal("events-jsonl must use the dedicated --events-jsonl flag")
	}
	var code int
	stderr := captureRunOutputStderr(t, func() {
		code = runAgent([]string{"--events-jsonl", "--output-format", "json", "task"}, "dev")
	})
	if code != 2 || !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
}

func captureRunOutputStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRunOutputJSONClassifiesRecoveryPauseAsControlledOutcome(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	runErr := fmt.Errorf("wrapped: %w", &agent.RecoveryPauseError{Message: "automatic recovery paused"})
	if err := sink.Finalize("abc", time.Now(), runErr); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.Subtype != event.TurnOutcomeRecoveryPaused || result.Result != runErr.Error() || result.NumTurns != 1 {
		t.Fatalf("recovery pause result = %+v", result)
	}
}

func TestRunOutputEventsJSONLClassifiesRecoveryPauseAsControlledOutcome(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputEventsJSONL)
	runErr := fmt.Errorf("wrapped: %w", &agent.RecoveryPauseError{Message: "automatic recovery paused"})
	if err := sink.Finalize("machine-session", time.Now(), runErr); err != nil {
		t.Fatal(err)
	}
	var result machineRunDone
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.NumTurns != 1 || result.SessionID != "machine-session" {
		t.Fatalf("recovery pause result = %+v", result)
	}
}

func TestRunOutputJSONPreservesCompletionUncertainAsControlledOutcome(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	runErr := &agent.CompletionUncertainError{Cause: agent.CompletionUncertainContextTool}
	if err := sink.Finalize("abc", time.Now(), runErr); err != nil {
		t.Fatal(err)
	}
	var result runResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.Subtype != event.TurnOutcomeCompletionUncertain || result.Result != runErr.Error() || result.NumTurns != 1 {
		t.Fatalf("completion uncertain result = %+v", result)
	}
}

func TestClassifyRunCompletion(t *testing.T) {
	pause := fmt.Errorf("wrapped: %w", &agent.RecoveryPauseError{Message: "paused"})
	if got := classifyRunCompletion(pause); got.outcome != event.TurnOutcomeRecoveryPaused || got.isError || got.exitCode != 0 {
		t.Fatalf("pause completion = %+v", got)
	}
	uncertain := fmt.Errorf("wrapped: %w", &agent.CompletionUncertainError{Cause: agent.CompletionUncertainContextTool})
	if got := classifyRunCompletion(uncertain); got.outcome != event.TurnOutcomeCompletionUncertain || got.subtype != event.TurnOutcomeCompletionUncertain || got.isError || got.exitCode != 1 {
		t.Fatalf("completion uncertain = %+v", got)
	}
	incomplete := fmt.Errorf("wrapped: %w", &agent.IncompleteReadError{Reason: "page budget"})
	if got := classifyRunCompletion(incomplete); got.outcome != event.TurnOutcomeIncompleteRead || got.subtype != event.TurnOutcomeIncompleteRead || got.isError || got.exitCode != 1 {
		t.Fatalf("incomplete read = %+v", got)
	}
	if got := classifyRunCompletion(errors.New("provider failed")); got.outcome != "" || !got.isError || got.exitCode != 1 {
		t.Fatalf("error completion = %+v", got)
	}
	if got := classifyRunCompletion(nil); got.outcome != "" || got.isError || got.exitCode != 0 {
		t.Fatalf("success completion = %+v", got)
	}
}
