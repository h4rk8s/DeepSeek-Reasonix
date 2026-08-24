package usageledger

import (
	"testing"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func quotedUsageEvent(model, source string, usage *provider.Usage, amount, currency string) event.Event {
	money := billing.Money{Amount: amount, Currency: currency}
	return event.Event{
		Kind:        event.Usage,
		ModelRef:    model,
		UsageSource: source,
		Usage:       usage,
		CostQuote: &billing.CostQuote{
			Original:           money,
			Selected:           &money,
			CostComplete:       true,
			DisplayComplete:    true,
			Complete:           true,
			DisplayStatus:      billing.DisplayStatusMatched,
			ModelRef:           "legacy/incorrect-model",
			UsageSource:        "legacy-incorrect-source",
			PricingFingerprint: model + ":" + source + ":" + currency,
		},
	}
}

func TestProjectionAttributesModelsAndSumsExactUSDTicks(t *testing.T) {
	l := New()
	l.Add(quotedUsageEvent("provider/planner", event.UsageSourcePlanner, &provider.Usage{PromptTokens: 3, CompletionTokens: 1, CacheMissTokens: 3}, "0.0000005", "USD"))
	l.Add(quotedUsageEvent("provider/executor", event.UsageSourceExecutor, &provider.Usage{PromptTokens: 7, CompletionTokens: 2, CacheHitTokens: 7}, "0.00000047", "USD"))
	p := l.Projection()
	if p.Usage.InputTokens != 10 || p.Usage.OutputTokens != 3 || len(p.ModelUsage) != 2 {
		t.Fatalf("projection = %+v", p)
	}
	// USD 0.0000005 + USD 0.00000047 = 9700 ticks at 1e-10 USD/tick.
	if p.TotalCostUSDTicks == nil || *p.TotalCostUSDTicks != 9700 || p.TotalCostUSD == nil || *p.TotalCostUSD != 0.00000097 {
		t.Fatalf("cost projection = %+v", p)
	}
	if _, ok := p.ModelUsage["planner:provider/planner"]; !ok {
		t.Fatalf("planner attribution used a non-canonical model identity: %+v", p.ModelUsage)
	}
	if _, ok := p.ModelUsage["executor:provider/executor"]; !ok {
		t.Fatalf("executor attribution used a non-canonical model identity: %+v", p.ModelUsage)
	}
}

func TestProjectionNeverRepricesProviderPricing(t *testing.T) {
	l := New()
	l.Add(event.Event{
		Kind: event.Usage, ModelRef: "provider/model", Usage: &provider.Usage{PromptTokens: 1_000_000},
		Pricing: &provider.Pricing{Input: 99, Currency: "USD"},
	})
	p := l.Projection()
	if !p.CostIsPartial || p.TotalCostUSD != nil || p.TotalCostUSDTicks != nil || p.Cost.CostQuote != nil {
		t.Fatalf("pricing without an occurrence-time quote was treated as cost truth: %+v", p)
	}
}

func TestProjectionFailsClosedWithoutCompleteUSDQuote(t *testing.T) {
	for _, e := range []event.Event{
		{Kind: event.Usage, ModelRef: "provider/model", Usage: &provider.Usage{PromptTokens: 100}},
		quotedUsageEvent("provider/model", event.UsageSourceExecutor, &provider.Usage{PromptTokens: 100}, "1", "CNY"),
	} {
		l := New()
		l.Add(e)
		p := l.Projection()
		if !p.CostIsPartial || p.TotalCostUSD != nil || p.TotalCostUSDTicks != nil {
			t.Fatalf("incomplete USD quote did not fail closed: %+v", p)
		}
	}
}

func TestProjectionMarksMissingAttributionAndOpenWorkIncomplete(t *testing.T) {
	l := New()
	l.Add(quotedUsageEvent("", event.UsageSourceExecutor, &provider.Usage{PromptTokens: 1}, "0.000001", "USD"))
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "running"}})
	p := l.Projection()
	if !p.UsageIsIncomplete || !p.CostIsPartial || p.TotalCostUSD != nil || p.OpenBackgroundSubagents != 1 || len(p.IncompleteReasons) != 2 {
		t.Fatalf("projection = %+v", p)
	}
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "done"}})
	p = l.Projection()
	if !p.UsageIsIncomplete || p.CostIsPartial || p.TotalCostUSD == nil || p.OpenBackgroundSubagents != 0 || len(p.IncompleteReasons) != 1 {
		t.Fatalf("completed projection = %+v", p)
	}
}

func TestProjectionKeepsTimedOutBackgroundSubagentFailClosed(t *testing.T) {
	l := New()
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "running"}})
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "drain_timeout"}})
	p := l.Projection()
	if !p.UsageIsIncomplete || p.OpenBackgroundSubagents != 1 || len(p.IncompleteReasons) != 3 {
		t.Fatalf("timed-out projection = %+v", p)
	}
}

func TestProjectionIgnoresBackgroundShellLifecycle(t *testing.T) {
	l := New()
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "bash-1", Kind: "bash", Status: "running"}})
	p := l.Projection()
	if !p.UsageIsIncomplete || !p.CostIsPartial || p.OpenBackgroundSubagents != 0 || len(p.IncompleteReasons) != 1 || p.IncompleteReasons[0] != "usage_not_reported" {
		t.Fatalf("shell projection = %+v", p)
	}
}

func TestProjectionFailsClosedWhenProviderReportsNoUsage(t *testing.T) {
	p := New().Projection()
	if !p.UsageIsIncomplete || !p.CostIsPartial || p.TotalCostUSD != nil || p.TotalCostUSDTicks != nil {
		t.Fatalf("empty projection did not fail closed: %+v", p)
	}
	if len(p.IncompleteReasons) != 1 || p.IncompleteReasons[0] != "usage_not_reported" {
		t.Fatalf("incomplete reasons = %v", p.IncompleteReasons)
	}
}

func TestSnapshotRestoreContinuesFromOccurrenceTimeQuotes(t *testing.T) {
	l := New()
	l.Add(quotedUsageEvent("provider/model", event.UsageSourceExecutor, &provider.Usage{PromptTokens: 1}, "0.25", "USD"))
	restored := Restore(l.Snapshot())
	restored.Add(quotedUsageEvent("provider/model", event.UsageSourceExecutor, &provider.Usage{CompletionTokens: 1}, "0.75", "USD"))
	p := restored.Projection()
	if p.Usage.InputTokens != 1 || p.Usage.OutputTokens != 1 || p.TotalCostUSDTicks == nil || *p.TotalCostUSDTicks != USDScale {
		t.Fatalf("restored projection = %+v", p)
	}
}
