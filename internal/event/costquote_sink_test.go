package event

import (
	"testing"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

func TestEnsureCostQuoteDoesNotUseRuntimeFX(t *testing.T) {
	e := Event{Kind: Usage, ModelRef: "deepseek/deepseek-v4-flash", UsageSource: UsageSourceExecutor,
		Pricing: &provider.Pricing{Input: 1, Output: 2, Currency: "CNY"},
		Usage:   &provider.Usage{PromptTokens: 100, CompletionTokens: 100}}
	ctx := &QuoteContext{DisplayCurrency: "USD"}
	q := EnsureCostQuote(e, ctx)
	if q == nil || q.Selected == nil || q.Selected.Currency != "CNY" || q.Complete {
		t.Fatalf("quote = %+v", q)
	}
	for code, valuation := range q.Valuations {
		if valuation.Basis == "fx" {
			t.Fatalf("runtime FX valuation %s = %+v", code, valuation)
		}
	}
}

func TestCostQuoteSinkRatesOnceAndPreservesExistingQuote(t *testing.T) {
	nowCalls := 0
	ctx := &QuoteContext{
		Now: func() time.Time {
			nowCalls++
			return time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
		},
		PricingContextForModel: func(string) billing.PricingContext {
			return billing.PricingContext{
				ProviderKind: "deepseek", ModelID: "deepseek-v4-pro", BillingMode: billing.BillingModePAYG,
				ScheduleID: billing.ScheduleDeepSeekV4August2026, CatalogSource: billing.DocDeepSeekPricing,
			}
		},
	}
	e := Event{Kind: Usage, ModelRef: "deepseek/deepseek-v4-pro",
		Pricing: &provider.Pricing{CacheHit: 0.30, Input: 9, Output: 27, Currency: "CNY"},
		Usage:   &provider.Usage{CompletionTokens: 1_000_000, TotalTokens: 1_000_000}}
	var first *billing.CostQuote
	sink := NewCostQuoteSink(FuncSink(func(got Event) { first = got.CostQuote }), ctx)
	sink.Emit(e)
	if nowCalls != 1 || first == nil || first.RateBand != billing.RateBandOffPeak || first.Original.Amount != "13.5" {
		t.Fatalf("quote=%+v nowCalls=%d", first, nowCalls)
	}

	prebuilt := &billing.CostQuote{Original: billing.Money{Amount: "7", Currency: "USD"}, RateBand: billing.RateBandPeak}
	e.CostQuote = prebuilt
	sink.Emit(e)
	if first != prebuilt || nowCalls != 1 {
		t.Fatalf("existing quote was recomputed: got=%p want=%p nowCalls=%d", first, prebuilt, nowCalls)
	}
}

func TestEnsureCostQuoteUsesProviderRequestStartInsteadOfCompletionClock(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, time.September, 10, 3, 59, 59, 0, time.UTC)
	completed := time.Date(2026, time.September, 10, 4, 0, 1, 0, time.UTC)
	schedule := billing.RateSchedule{
		ID: "boundary", EffectiveFrom: started.Add(-time.Hour), Location: shanghai,
		PeakWeekdays: []time.Weekday{time.Thursday}, PeakWindows: []billing.DailyRateWindow{{StartMinute: 9 * 60, EndMinute: 12 * 60}},
		Peak: billing.RateCard{Input: 2, Currency: "CNY"}, OffPeak: billing.RateCard{Input: 1, Currency: "CNY"},
	}
	ctx := &QuoteContext{
		Now: func() time.Time { return completed },
		PricingContextForModel: func(string) billing.PricingContext {
			return billing.PricingContext{RateSchedules: []billing.RateSchedule{schedule}}
		},
	}
	e := Event{
		Kind: Usage, ModelRef: "deepseek/deepseek-v4-flash",
		Usage:   &provider.Usage{PromptTokens: 1_000_000, RequestStartedAt: started.UnixMilli()},
		Pricing: &provider.Pricing{Input: 99, Currency: "USD"},
	}
	q := EnsureCostQuote(e, ctx)
	if q == nil || q.RateBand != billing.RateBandPeak || q.Original.Currency != "CNY" || q.Original.Amount != "2" {
		t.Fatalf("quote = %+v", q)
	}
	if q.RatedAt != started.Format(time.RFC3339Nano) {
		t.Fatalf("rated_at = %q, want request start %q", q.RatedAt, started.Format(time.RFC3339Nano))
	}
}
