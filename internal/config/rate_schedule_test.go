package config

import (
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

func TestProviderRateSchedulesDecodeRenderAndResolve(t *testing.T) {
	raw := `
config_version = 9
default_model = "deepseek-flash/deepseek-v4-flash"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"]
default = "deepseek-v4-flash"
price = { cache_hit = 0.04, input = 2, output = 8, currency = "CNY" }
billing_currency = "CNY"

[[providers.rate_schedules]]
id = "deepseek-flash-2026-09-10"
models = ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"]
effective_from = "2026-09-10T12:00:00+08:00"
timezone = "Asia/Shanghai"
peak_weekdays = ["mon", "tue", "wed", "thu", "fri"]
peak_windows = ["09:00-12:00", "14:00-18:00"]
peak = { cache_hit = 0.04, input = 2, output = 8, currency = "CNY" }
off_peak = { cache_hit = 0.02, input = 1, output = 4, currency = "CNY" }
source = "https://example.test/deepseek-flash-pricing"
`
	cfg := &Config{}
	if _, err := toml.Decode(raw, cfg); err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("deepseek-flash")
	if !ok || len(p.RateSchedules) != 1 {
		t.Fatalf("provider schedules = %+v", p)
	}
	if err := validateProvider(*p); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	ctx := p.PricingContextForModel("deepseek-v4-flash-vision-exp")
	if len(ctx.RateSchedules) != 1 {
		t.Fatalf("pricing context = %+v", ctx)
	}
	resolved, ok := billing.ResolveConfiguredRate(ctx.RateSchedules, time.Date(2026, time.September, 10, 6, 0, 0, 0, time.UTC))
	if !ok || resolved.RateBand != billing.RateBandPeak || resolved.Card.Output != 8 {
		t.Fatalf("resolved = %+v, ok=%v", resolved, ok)
	}

	rendered := RenderTOML(cfg)
	if !strings.Contains(rendered, "[[providers.rate_schedules]]") || !strings.Contains(rendered, `off_peak = { cache_hit = 0.02, input = 1, output = 4, currency = "¥" }`) {
		t.Fatalf("schedule omitted from render:\n%s", rendered)
	}
	decoded := &Config{}
	if _, err := toml.Decode(rendered, decoded); err != nil {
		t.Fatalf("rendered config does not parse: %v\n%s", err, rendered)
	}
	got, _ := decoded.Provider("deepseek-flash")
	if got == nil || len(got.RateSchedules) != 1 || got.RateSchedules[0].ID != "deepseek-flash-2026-09-10" {
		t.Fatalf("round trip schedules = %+v", got)
	}
}

func pricing(cacheHit, input, output float64, currency string) *provider.Pricing {
	return &provider.Pricing{CacheHit: cacheHit, Input: input, Output: output, Currency: currency}
}

func TestProviderRateScheduleValidationRejectsAmbiguousOrMalformedRules(t *testing.T) {
	valid := ProviderRateSchedule{
		ID: "one", Models: []string{"model"}, EffectiveFrom: "2026-09-10T12:00:00+08:00",
		Timezone: "Asia/Shanghai", PeakWeekdays: []string{"mon"}, PeakWindows: []string{"09:00-12:00"},
		Peak: pricing(1, 2, 3, "CNY"), OffPeak: pricing(0.5, 1, 1.5, "CNY"),
	}
	tests := []struct {
		name      string
		schedules []ProviderRateSchedule
	}{
		{"bad timezone", []ProviderRateSchedule{func() ProviderRateSchedule { s := valid; s.Timezone = "Mars/Base"; return s }()}},
		{"bad window", []ProviderRateSchedule{func() ProviderRateSchedule { s := valid; s.PeakWindows = []string{"12:00-09:00"}; return s }()}},
		{"missing rate", []ProviderRateSchedule{func() ProviderRateSchedule { s := valid; s.OffPeak = nil; return s }()}},
		{"overlap", []ProviderRateSchedule{valid, func() ProviderRateSchedule { s := valid; s.ID = "two"; return s }()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := ProviderEntry{Name: "p", Kind: "openai", BaseURL: "https://example.test", Model: "model", BillingCurrency: "CNY", RateSchedules: tc.schedules}
			if err := validateProvider(p); err == nil {
				t.Fatalf("invalid schedules accepted: %+v", tc.schedules)
			}
		})
	}
}
