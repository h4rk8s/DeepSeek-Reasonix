package billing

import "time"

// DailyRateWindow is a half-open local-time interval measured in minutes
// after midnight. Config parsing rejects overnight and zero-length windows.
type DailyRateWindow struct {
	StartMinute int
	EndMinute   int
}

// RateSchedule is a host-resolved, configuration-owned price version. It is
// deliberately provider-neutral: vendor prices and cutovers stay in config.
type RateSchedule struct {
	ID            string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
	Location      *time.Location
	PeakWeekdays  []time.Weekday
	PeakWindows   []DailyRateWindow
	Peak          RateCard
	OffPeak       RateCard
	Source        string
}

func (s RateSchedule) effective(at time.Time) bool {
	if at.Before(s.EffectiveFrom) {
		return false
	}
	return s.EffectiveTo.IsZero() || at.Before(s.EffectiveTo)
}

func (s RateSchedule) rateBand(at time.Time) string {
	location := s.Location
	if location == nil {
		location = time.UTC
	}
	local := at.In(location)
	weekdayPeak := false
	for _, day := range s.PeakWeekdays {
		if local.Weekday() == day {
			weekdayPeak = true
			break
		}
	}
	if !weekdayPeak {
		return RateBandOffPeak
	}
	minute := local.Hour()*60 + local.Minute()
	for _, window := range s.PeakWindows {
		if minute >= window.StartMinute && minute < window.EndMinute {
			return RateBandPeak
		}
	}
	return RateBandOffPeak
}

// ResolveConfiguredRate selects the latest applicable configuration version
// and then its local peak/off-peak band. Config validation rejects overlapping
// versions; latest-from selection remains deterministic for older callers that
// construct schedules directly.
func ResolveConfiguredRate(schedules []RateSchedule, at time.Time) (ResolvedRate, bool) {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	var selected *RateSchedule
	for i := range schedules {
		schedule := &schedules[i]
		if !schedule.effective(at) {
			continue
		}
		if selected == nil || schedule.EffectiveFrom.After(selected.EffectiveFrom) {
			selected = schedule
		}
	}
	if selected == nil {
		return ResolvedRate{}, false
	}
	band := selected.rateBand(at)
	card := selected.OffPeak
	if band == RateBandPeak {
		card = selected.Peak
	}
	return ResolvedRate{
		Card: card, RateBand: band, ScheduleID: selected.ID, OccurredAt: at, Source: selected.Source,
	}, true
}
