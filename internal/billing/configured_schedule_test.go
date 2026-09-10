package billing

import (
	"testing"
	"time"
)

func TestResolveConfiguredRateScheduleVersionsAndWindows(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	windows := []DailyRateWindow{{StartMinute: 9 * 60, EndMinute: 12 * 60}, {StartMinute: 14 * 60, EndMinute: 18 * 60}}
	cutover := time.Date(2026, time.September, 10, 4, 0, 0, 0, time.UTC)
	schedules := []RateSchedule{
		{
			ID: "deepseek-flash-2026-08-17", EffectiveFrom: time.Date(2026, time.August, 16, 16, 0, 0, 0, time.UTC),
			EffectiveTo: cutover, Location: shanghai, PeakWeekdays: weekdays, PeakWindows: windows,
			Peak:    RateCard{CacheHit: 0.10, Input: 3, Output: 9, Currency: "CNY"},
			OffPeak: RateCard{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "CNY"},
		},
		{
			ID: "deepseek-flash-2026-09-10", EffectiveFrom: cutover, Location: shanghai,
			PeakWeekdays: weekdays, PeakWindows: windows,
			Peak:    RateCard{CacheHit: 0.04, Input: 2, Output: 8, Currency: "CNY"},
			OffPeak: RateCard{CacheHit: 0.02, Input: 1, Output: 4, Currency: "CNY"},
		},
	}

	tests := []struct {
		name string
		at   string
		id   string
		band string
		want RateCard
	}{
		{"old peak immediately before cutover", "2026-09-10T03:59:59Z", "deepseek-flash-2026-08-17", RateBandPeak, schedules[0].Peak},
		{"new off peak at cutover", "2026-09-10T04:00:00Z", "deepseek-flash-2026-09-10", RateBandOffPeak, schedules[1].OffPeak},
		{"new afternoon peak", "2026-09-10T06:00:00Z", "deepseek-flash-2026-09-10", RateBandPeak, schedules[1].Peak},
		{"new weekend off peak", "2026-09-12T06:00:00Z", "deepseek-flash-2026-09-10", RateBandOffPeak, schedules[1].OffPeak},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := ResolveConfiguredRate(schedules, at)
			if !ok || got.ScheduleID != tc.id || got.RateBand != tc.band || got.Card != tc.want {
				t.Fatalf("resolved = %+v, ok=%v; want id=%q band=%q card=%+v", got, ok, tc.id, tc.band, tc.want)
			}
		})
	}
}

func TestBuildQuotePrefersExplicitConfiguredSchedule(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.September, 10, 4, 0, 0, 0, time.UTC)
	schedule := RateSchedule{
		ID: "config-owned", EffectiveFrom: at, Location: shanghai,
		PeakWeekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		PeakWindows:  []DailyRateWindow{{StartMinute: 9 * 60, EndMinute: 12 * 60}},
		Peak:         RateCard{CacheHit: 0.04, Input: 2, Output: 8, Currency: "CNY"},
		OffPeak:      RateCard{CacheHit: 0.02, Input: 1, Output: 4, Currency: "CNY"},
		Source:       "https://example.test/pricing-notice",
	}
	q := BuildQuote(QuoteInput{
		Usage: UsageTokens{PromptTokens: 2_000_000, CacheHitTokens: 1_000_000, CacheMissTokens: 1_000_000, CompletionTokens: 1_000_000},
		// Deliberately unrelated: the explicit schedule is the authoritative rate card.
		Rates:      RateCard{CacheHit: 99, Input: 99, Output: 99, Currency: "USD"},
		OccurredAt: at, RateSchedules: []RateSchedule{schedule},
	})
	if q.Original.Currency != "CNY" || q.Original.Amount != "5.02" || q.RateBand != RateBandOffPeak {
		t.Fatalf("quote = %+v", q)
	}
	if q.RateScheduleID != "config-owned" || q.CatalogSource != schedule.Source || q.RatedAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("schedule provenance = %+v", q)
	}
}
