package config

import (
	"fmt"
	"strings"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

// ProviderRateSchedule keeps vendor prices and cutovers in TOML rather than
// source code. Models empty means every model exposed by the provider entry.
type ProviderRateSchedule struct {
	ID            string            `toml:"id"`
	Models        []string          `toml:"models"`
	EffectiveFrom string            `toml:"effective_from"`
	EffectiveTo   string            `toml:"effective_to"`
	Timezone      string            `toml:"timezone"`
	PeakWeekdays  []string          `toml:"peak_weekdays"`
	PeakWindows   []string          `toml:"peak_windows"`
	Peak          *provider.Pricing `toml:"peak"`
	OffPeak       *provider.Pricing `toml:"off_peak"`
	Source        string            `toml:"source"`
}

func (s ProviderRateSchedule) appliesTo(model string) bool {
	if len(s.Models) == 0 {
		return true
	}
	model = strings.TrimSpace(model)
	for _, candidate := range s.Models {
		if strings.TrimSpace(candidate) == model {
			return true
		}
	}
	return false
}

func compileProviderRateSchedule(s ProviderRateSchedule) (billing.RateSchedule, error) {
	id := strings.TrimSpace(s.ID)
	if id == "" {
		return billing.RateSchedule{}, fmt.Errorf("id is required")
	}
	from, err := time.Parse(time.RFC3339, strings.TrimSpace(s.EffectiveFrom))
	if err != nil {
		return billing.RateSchedule{}, fmt.Errorf("effective_from must be RFC3339: %w", err)
	}
	var to time.Time
	if raw := strings.TrimSpace(s.EffectiveTo); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return billing.RateSchedule{}, fmt.Errorf("effective_to must be RFC3339: %w", err)
		}
		if !to.After(from) {
			return billing.RateSchedule{}, fmt.Errorf("effective_to must be after effective_from")
		}
	}
	zone := strings.TrimSpace(s.Timezone)
	if zone == "" {
		return billing.RateSchedule{}, fmt.Errorf("timezone is required")
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return billing.RateSchedule{}, fmt.Errorf("timezone %q: %w", zone, err)
	}
	weekdays := make([]time.Weekday, 0, len(s.PeakWeekdays))
	seenDays := map[time.Weekday]bool{}
	for _, raw := range s.PeakWeekdays {
		day, ok := parseRateWeekday(raw)
		if !ok {
			return billing.RateSchedule{}, fmt.Errorf("unknown peak weekday %q", raw)
		}
		if !seenDays[day] {
			weekdays = append(weekdays, day)
			seenDays[day] = true
		}
	}
	if len(weekdays) == 0 {
		return billing.RateSchedule{}, fmt.Errorf("peak_weekdays is required")
	}
	windows := make([]billing.DailyRateWindow, 0, len(s.PeakWindows))
	for _, raw := range s.PeakWindows {
		window, err := parseDailyRateWindow(raw)
		if err != nil {
			return billing.RateSchedule{}, err
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return billing.RateSchedule{}, fmt.Errorf("peak_windows is required")
	}
	peak, err := configRateCard(s.Peak)
	if err != nil {
		return billing.RateSchedule{}, fmt.Errorf("peak: %w", err)
	}
	offPeak, err := configRateCard(s.OffPeak)
	if err != nil {
		return billing.RateSchedule{}, fmt.Errorf("off_peak: %w", err)
	}
	if peak.Currency != offPeak.Currency {
		return billing.RateSchedule{}, fmt.Errorf("peak and off_peak currencies differ")
	}
	return billing.RateSchedule{
		ID: id, EffectiveFrom: from.UTC(), EffectiveTo: to.UTC(), Location: location,
		PeakWeekdays: weekdays, PeakWindows: windows, Peak: peak, OffPeak: offPeak,
		Source: strings.TrimSpace(s.Source),
	}, nil
}

func configRateCard(price *provider.Pricing) (billing.RateCard, error) {
	if price == nil {
		return billing.RateCard{}, fmt.Errorf("rate card is required")
	}
	currency := billing.NormalizeCurrency(price.Currency)
	if currency == "" {
		return billing.RateCard{}, fmt.Errorf("currency is required")
	}
	if price.CacheHit < 0 || price.Input <= 0 || price.Output <= 0 {
		return billing.RateCard{}, fmt.Errorf("rates must be non-negative and input/output must be positive")
	}
	return billing.RateCard{CacheHit: price.CacheHit, Input: price.Input, Output: price.Output, Currency: currency}, nil
}

func parseRateWeekday(raw string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sun", "sunday":
		return time.Sunday, true
	case "mon", "monday":
		return time.Monday, true
	case "tue", "tues", "tuesday":
		return time.Tuesday, true
	case "wed", "wednesday":
		return time.Wednesday, true
	case "thu", "thur", "thurs", "thursday":
		return time.Thursday, true
	case "fri", "friday":
		return time.Friday, true
	case "sat", "saturday":
		return time.Saturday, true
	default:
		return 0, false
	}
}

func parseDailyRateWindow(raw string) (billing.DailyRateWindow, error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) != 2 {
		return billing.DailyRateWindow{}, fmt.Errorf("peak window %q must be HH:MM-HH:MM", raw)
	}
	parseMinute := func(value string) (int, error) {
		parsed, err := time.Parse("15:04", strings.TrimSpace(value))
		if err != nil {
			return 0, err
		}
		return parsed.Hour()*60 + parsed.Minute(), nil
	}
	start, err := parseMinute(parts[0])
	if err != nil {
		return billing.DailyRateWindow{}, fmt.Errorf("peak window %q: %w", raw, err)
	}
	end, err := parseMinute(parts[1])
	if err != nil {
		return billing.DailyRateWindow{}, fmt.Errorf("peak window %q: %w", raw, err)
	}
	if end <= start {
		return billing.DailyRateWindow{}, fmt.Errorf("peak window %q must end after it starts", raw)
	}
	return billing.DailyRateWindow{StartMinute: start, EndMinute: end}, nil
}

func (e *ProviderEntry) rateSchedulesForModel(model string) ([]billing.RateSchedule, error) {
	if e == nil {
		return nil, nil
	}
	out := make([]billing.RateSchedule, 0, len(e.RateSchedules))
	for _, raw := range e.RateSchedules {
		if !raw.appliesTo(model) {
			continue
		}
		schedule, err := compileProviderRateSchedule(raw)
		if err != nil {
			return nil, fmt.Errorf("rate_schedule %q: %w", raw.ID, err)
		}
		out = append(out, schedule)
	}
	return out, nil
}

func validateProviderRateSchedules(e ProviderEntry) error {
	seenIDs := map[string]bool{}
	compiled := make([]billing.RateSchedule, len(e.RateSchedules))
	for i, raw := range e.RateSchedules {
		schedule, err := compileProviderRateSchedule(raw)
		if err != nil {
			return fmt.Errorf("provider %q rate_schedule %q: %w", e.Name, raw.ID, err)
		}
		if seenIDs[schedule.ID] {
			return fmt.Errorf("provider %q rate_schedule id %q is duplicated", e.Name, schedule.ID)
		}
		seenIDs[schedule.ID] = true
		compiled[i] = schedule
		if cur := e.ProviderBillingCurrency(); cur != "" && schedule.Peak.Currency != cur {
			return fmt.Errorf("provider %q rate_schedule %q currency %s differs from billing_currency %s", e.Name, schedule.ID, schedule.Peak.Currency, cur)
		}
	}
	for i := range compiled {
		for j := i + 1; j < len(compiled); j++ {
			if rateScheduleModelsOverlap(e.RateSchedules[i], e.RateSchedules[j]) && rateScheduleTimesOverlap(compiled[i], compiled[j]) {
				return fmt.Errorf("provider %q rate_schedules %q and %q overlap", e.Name, compiled[i].ID, compiled[j].ID)
			}
		}
	}
	return nil
}

func rateScheduleModelsOverlap(a, b ProviderRateSchedule) bool {
	if len(a.Models) == 0 || len(b.Models) == 0 {
		return true
	}
	for _, model := range a.Models {
		if b.appliesTo(model) {
			return true
		}
	}
	return false
}

func rateScheduleTimesOverlap(a, b billing.RateSchedule) bool {
	aEndsAfterBStarts := a.EffectiveTo.IsZero() || a.EffectiveTo.After(b.EffectiveFrom)
	bEndsAfterAStarts := b.EffectiveTo.IsZero() || b.EffectiveTo.After(a.EffectiveFrom)
	return aEndsAfterBStarts && bEndsAfterAStarts
}
