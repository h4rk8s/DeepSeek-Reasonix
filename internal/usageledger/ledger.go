// Package usageledger provides one fail-closed projection for billable model usage.
package usageledger

import (
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const USDScale int64 = 10_000_000_000

const ledgerSnapshotVersion int = 1

type Tokens struct {
	InputTokens              int  `json:"input_tokens"`
	OutputTokens             int  `json:"output_tokens"`
	ReasoningTokens          int  `json:"reasoning_tokens,omitempty"`
	CacheReadInputTokens     int  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int  `json:"cache_creation_input_tokens"`
	Estimated                bool `json:"estimated,omitempty"`
}

type ModelUsage struct {
	Model         string `json:"model,omitempty"`
	Source        string `json:"source"`
	Usage         Tokens `json:"usage"`
	CostIsPartial bool   `json:"cost_is_partial"`
	CostUSDTicks  *int64 `json:"cost_usd_ticks,omitempty"`
}

// CostProjection is the compatibility view of the same occurrence-time quote
// ledger used for exact USD totals. Protocol adapters project these fields
// into their established wire schemas.
type CostProjection struct {
	TotalCost       *float64
	Currency        string
	CostComplete    bool
	DisplayComplete bool
	DisplayStatus   string
	AggregateMode   string
	OriginalCosts   map[string]float64
	OriginalTotals  []billing.Money
	CostQuote       *billing.CostQuote
}

type Projection struct {
	Usage                   Tokens                `json:"usage"`
	ModelUsage              map[string]ModelUsage `json:"modelUsage"`
	UsageIsIncomplete       bool                  `json:"usage_is_incomplete"`
	CostIsPartial           bool                  `json:"cost_is_partial"`
	TotalCostUSDTicks       *int64                `json:"total_cost_usd_ticks,omitempty"`
	TotalCostUSD            *float64              `json:"total_cost_usd,omitempty"`
	IncompleteReasons       []string              `json:"incomplete_reasons,omitempty"`
	OpenBackgroundSubagents int                   `json:"open_background_subagents,omitempty"`
	Cost                    CostProjection        `json:"-"`
}

type row struct {
	model, source string
	tokens        Tokens
	usageEvents   int
	quoteEvents   int
}

// RowSnapshot is one model/source bucket in a persisted ledger snapshot.
type RowSnapshot struct {
	Model       string `json:"model,omitempty"`
	Source      string `json:"source"`
	Usage       Tokens `json:"usage"`
	UsageEvents int    `json:"usageEvents"`
	QuoteEvents int    `json:"quoteEvents"`
}

// Snapshot preserves owner state without requiring callers to maintain a
// second scalar cost accumulator. It is additive to existing persisted wires.
type Snapshot struct {
	SchemaVersion           int                    `json:"schemaVersion"`
	Usage                   Tokens                 `json:"usage"`
	Rows                    map[string]RowSnapshot `json:"rows"`
	QuoteLedger             *billing.Ledger        `json:"quoteLedger,omitempty"`
	IncompleteReasons       []string               `json:"incompleteReasons,omitempty"`
	OpenBackgroundSubagents []string               `json:"openBackgroundSubagents,omitempty"`
	DrainTimedOut           bool                   `json:"drainTimedOut,omitempty"`
	UsageEvents             int                    `json:"usageEvents"`
	QuoteEvents             int                    `json:"quoteEvents"`
}

type Ledger struct {
	mu                sync.Mutex
	tokens            Tokens
	rows              map[string]*row
	quoteLedger       *billing.Ledger
	usageEvents       int
	quoteEvents       int
	incompleteReasons map[string]struct{}
	openBackground    map[string]struct{}
	drainTimedOut     bool
}

func New() *Ledger {
	return &Ledger{
		rows:              map[string]*row{},
		quoteLedger:       billing.NewLedger(),
		incompleteReasons: map[string]struct{}{},
		openBackground:    map[string]struct{}{},
	}
}

func (l *Ledger) Add(e event.Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.Kind == event.BackgroundJobLifecycle {
		l.addBackgroundJob(e.BackgroundJob)
		return
	}
	if e.Kind != event.Usage || e.Usage == nil {
		return
	}
	l.ensureMaps()
	t := tokens(e.Usage)
	addTokens(&l.tokens, t)
	l.usageEvents++

	source := normalizeSource(e.UsageSource)
	model := strings.TrimSpace(e.ModelRef)
	key := source + ":" + model
	r := l.rows[key]
	if r == nil {
		r = &row{model: model, source: source}
		l.rows[key] = r
	}
	addTokens(&r.tokens, t)
	r.usageEvents++
	if model == "" {
		l.incompleteReasons["missing_model_attribution"] = struct{}{}
	}
	if e.CostQuote == nil {
		return
	}

	q := cloneCostQuote(*e.CostQuote)
	q.ModelRef = model
	q.UsageSource = source
	l.quoteLedger.Add(q, billingTokens(e.Usage), quoteOccurredAt(q))
	l.quoteEvents++
	r.quoteEvents++
}

func (l *Ledger) ensureMaps() {
	if l.rows == nil {
		l.rows = map[string]*row{}
	}
	if l.quoteLedger == nil {
		l.quoteLedger = billing.NewLedger()
	}
	if l.incompleteReasons == nil {
		l.incompleteReasons = map[string]struct{}{}
	}
	if l.openBackground == nil {
		l.openBackground = map[string]struct{}{}
	}
}

func (l *Ledger) addBackgroundJob(job event.BackgroundJob) {
	if strings.TrimSpace(job.Kind) != "task" || strings.TrimSpace(job.ID) == "" {
		return
	}
	if l.openBackground == nil {
		l.openBackground = map[string]struct{}{}
	}
	switch strings.TrimSpace(job.Status) {
	case "running":
		l.openBackground[job.ID] = struct{}{}
	case "done", "failed", "killed":
		delete(l.openBackground, job.ID)
	case "drain_timeout":
		l.openBackground[job.ID] = struct{}{}
		l.drainTimedOut = true
	}
}

func (l *Ledger) MarkIncomplete(reason string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.incompleteReasons == nil {
		l.incompleteReasons = map[string]struct{}{}
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		l.incompleteReasons[reason] = struct{}{}
	}
}

func (l *Ledger) Projection() Projection {
	if l == nil {
		return emptyProjection()
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	p := Projection{
		Usage:                   l.tokens,
		ModelUsage:              make(map[string]ModelUsage, len(l.rows)),
		OpenBackgroundSubagents: len(l.openBackground),
	}
	if len(l.rows) == 0 {
		p.IncompleteReasons = append(p.IncompleteReasons, "usage_not_reported")
	}
	for reason := range l.incompleteReasons {
		p.IncompleteReasons = append(p.IncompleteReasons, reason)
	}
	pendingBackground := len(l.openBackground) > 0 || l.drainTimedOut
	if len(l.openBackground) > 0 {
		p.IncompleteReasons = append(p.IncompleteReasons, "background_subagent_usage_pending")
	}
	if l.drainTimedOut {
		p.IncompleteReasons = append(p.IncompleteReasons, "background_subagent_drain_timeout")
	}
	sort.Strings(p.IncompleteReasons)
	p.UsageIsIncomplete = len(p.IncompleteReasons) > 0

	for key, r := range l.rows {
		mr := ModelUsage{Model: r.model, Source: r.source, Usage: r.tokens}
		usd := billing.AggregateQuotes(l.quotesFor(r.model, r.source), "USD")
		complete, ticks := completeUSDTicks(usd, r.usageEvents, r.quoteEvents, false)
		mr.CostIsPartial = !complete
		if complete {
			mr.CostUSDTicks = &ticks
		}
		p.ModelUsage[key] = mr
	}

	p.Cost = l.costProjection(pendingBackground)
	usd := l.quoteLedger.Total("USD")
	complete, ticks := completeUSDTicks(usd, l.usageEvents, l.quoteEvents, pendingBackground)
	p.CostIsPartial = !complete
	if complete {
		value := float64(ticks) / float64(USDScale)
		p.TotalCostUSDTicks = &ticks
		p.TotalCostUSD = &value
	}
	return p
}

func emptyProjection() Projection {
	return Projection{
		ModelUsage:        map[string]ModelUsage{},
		UsageIsIncomplete: true,
		CostIsPartial:     true,
		IncompleteReasons: []string{"usage_not_reported"},
	}
}

func (l *Ledger) costProjection(pendingBackground bool) CostProjection {
	if l.quoteLedger == nil || len(l.quoteLedger.Entries) == 0 {
		return CostProjection{}
	}
	agg := l.quoteLedger.Total("")
	allQuoted := l.usageEvents > 0 && l.quoteEvents == l.usageEvents
	if !allQuoted || pendingBackground {
		agg.CostComplete = false
		agg.DisplayComplete = false
		agg.Complete = false
		agg.DisplayStatus = billing.DisplayStatusUnavailable
		if agg.IncompleteReason == "" {
			if pendingBackground {
				agg.IncompleteReason = "background_subagent_usage_pending"
			} else {
				agg.IncompleteReason = "missing_occurrence_cost_quote"
			}
		}
	}
	cost := CostProjection{
		CostComplete:    agg.CostComplete,
		DisplayComplete: agg.DisplayComplete,
		DisplayStatus:   agg.DisplayStatus,
		AggregateMode:   agg.AggregateMode,
		OriginalCosts:   originalCosts(agg),
		OriginalTotals:  append([]billing.Money(nil), agg.OriginalTotals...),
		CostQuote:       &agg,
	}
	if allQuoted && !pendingBackground && agg.Selected != nil {
		value := agg.Selected.Float64()
		cost.TotalCost = &value
		cost.Currency = agg.LegacyCurrencyCode()
	}
	return cost
}

func (l *Ledger) quotesFor(model, source string) []billing.CostQuote {
	if l.quoteLedger == nil {
		return nil
	}
	quotes := make([]billing.CostQuote, 0)
	for _, entry := range l.quoteLedger.Entries {
		if entry.ModelRef == model && normalizeSource(entry.UsageSource) == source {
			quotes = append(quotes, cloneCostQuote(entry.Quote))
		}
	}
	return quotes
}

func completeUSDTicks(q billing.CostQuote, usageEvents, quoteEvents int, pending bool) (bool, int64) {
	if pending || usageEvents == 0 || quoteEvents != usageEvents || !q.CostComplete || !q.DisplayComplete || q.Selected == nil || billing.NormalizeCurrency(q.Selected.Currency) != "USD" {
		return false, 0
	}
	ticks, ok := moneyTicks(*q.Selected)
	return ok, ticks
}

func moneyTicks(m billing.Money) (int64, bool) {
	if billing.NormalizeCurrency(m.Currency) != "USD" {
		return 0, false
	}
	value := new(big.Rat)
	if _, ok := value.SetString(strings.TrimSpace(m.Amount)); !ok {
		return 0, false
	}
	scaled := new(big.Rat).Mul(value, big.NewRat(USDScale, 1))
	n := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	rem := new(big.Int).Rem(scaled.Num(), scaled.Denom())
	if new(big.Int).Lsh(new(big.Int).Abs(rem), 1).Cmp(new(big.Int).Abs(scaled.Denom())) >= 0 {
		if scaled.Sign() >= 0 {
			n.Add(n, big.NewInt(1))
		} else {
			n.Sub(n, big.NewInt(1))
		}
	}
	if !n.IsInt64() {
		return 0, false
	}
	return n.Int64(), true
}

func originalCosts(q billing.CostQuote) map[string]float64 {
	totals := q.OriginalTotals
	if len(totals) == 0 && billing.NormalizeCurrency(q.Original.Currency) != "" {
		totals = []billing.Money{q.Original}
	}
	if len(totals) == 0 {
		return nil
	}
	out := make(map[string]float64, len(totals))
	for _, total := range totals {
		if currency := billing.NormalizeCurrency(total.Currency); currency != "" {
			out[currency] += total.Float64()
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tokens(u *provider.Usage) Tokens {
	return Tokens{
		InputTokens:              u.PromptTokens,
		OutputTokens:             u.CompletionTokens,
		ReasoningTokens:          u.ReasoningTokens,
		CacheReadInputTokens:     u.CacheHitTokens,
		CacheCreationInputTokens: u.CacheMissTokens,
		Estimated:                u.Estimated,
	}
}

func addTokens(dst *Tokens, src Tokens) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.ReasoningTokens += src.ReasoningTokens
	dst.CacheReadInputTokens += src.CacheReadInputTokens
	dst.CacheCreationInputTokens += src.CacheCreationInputTokens
	dst.Estimated = dst.Estimated || src.Estimated
}

func billingTokens(u *provider.Usage) billing.UsageTokens {
	return billing.UsageTokens{
		PromptTokens:           u.PromptTokens,
		CompletionTokens:       u.CompletionTokens,
		CacheHitTokens:         u.CacheHitTokens,
		CacheMissTokens:        u.CacheMissTokens,
		CacheWriteTokens:       u.CacheWriteTokens,
		CacheWriteBilledTokens: u.CacheWriteBilledTokens,
		Estimated:              u.Estimated,
	}
}

func normalizeSource(source string) string {
	if source = strings.TrimSpace(source); source != "" {
		return source
	}
	return event.UsageSourceExecutor
}

func quoteOccurredAt(q billing.CostQuote) time.Time {
	if q.RatedAt != "" {
		if occurred, err := time.Parse(time.RFC3339Nano, q.RatedAt); err == nil {
			return occurred.UTC()
		}
	}
	return time.Now().UTC()
}

// Snapshot returns an independent copy suitable for persistence.
func (l *Ledger) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{SchemaVersion: ledgerSnapshotVersion, Rows: map[string]RowSnapshot{}}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := Snapshot{
		SchemaVersion: ledgerSnapshotVersion,
		Usage:         l.tokens,
		Rows:          make(map[string]RowSnapshot, len(l.rows)),
		QuoteLedger:   cloneBillingLedger(l.quoteLedger),
		DrainTimedOut: l.drainTimedOut,
		UsageEvents:   l.usageEvents,
		QuoteEvents:   l.quoteEvents,
	}
	for key, r := range l.rows {
		snapshot.Rows[key] = RowSnapshot{
			Model: r.model, Source: r.source, Usage: r.tokens,
			UsageEvents: r.usageEvents, QuoteEvents: r.quoteEvents,
		}
	}
	for reason := range l.incompleteReasons {
		snapshot.IncompleteReasons = append(snapshot.IncompleteReasons, reason)
	}
	for id := range l.openBackground {
		snapshot.OpenBackgroundSubagents = append(snapshot.OpenBackgroundSubagents, id)
	}
	sort.Strings(snapshot.IncompleteReasons)
	sort.Strings(snapshot.OpenBackgroundSubagents)
	return snapshot
}

// Restore reconstructs a ledger from an owner snapshot. Unknown future
// versions fail closed by preserving token rows while discarding cost facts.
func Restore(snapshot Snapshot) *Ledger {
	l := New()
	l.tokens = snapshot.Usage
	l.usageEvents = snapshot.UsageEvents
	l.quoteEvents = snapshot.QuoteEvents
	l.drainTimedOut = snapshot.DrainTimedOut
	for key, saved := range snapshot.Rows {
		source := normalizeSource(saved.Source)
		model := strings.TrimSpace(saved.Model)
		canonicalKey := source + ":" + model
		if strings.TrimSpace(key) == canonicalKey {
			canonicalKey = key
		}
		l.rows[canonicalKey] = &row{
			model: model, source: source, tokens: saved.Usage,
			usageEvents: saved.UsageEvents, quoteEvents: saved.QuoteEvents,
		}
	}
	for _, reason := range snapshot.IncompleteReasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			l.incompleteReasons[reason] = struct{}{}
		}
	}
	for _, id := range snapshot.OpenBackgroundSubagents {
		if id = strings.TrimSpace(id); id != "" {
			l.openBackground[id] = struct{}{}
		}
	}
	if snapshot.SchemaVersion > ledgerSnapshotVersion {
		l.quoteLedger = billing.NewLedger()
		l.quoteEvents = 0
		for _, r := range l.rows {
			r.quoteEvents = 0
		}
		l.incompleteReasons["unsupported_usage_ledger_snapshot"] = struct{}{}
	} else {
		l.quoteLedger = cloneBillingLedger(snapshot.QuoteLedger)
		if l.quoteLedger == nil {
			l.quoteLedger = billing.NewLedger()
		}
	}
	return l
}

func cloneBillingLedger(in *billing.Ledger) *billing.Ledger {
	if in == nil {
		return nil
	}
	out := &billing.Ledger{Version: in.Version, Entries: make(map[string]billing.LedgerEntry, len(in.Entries))}
	for key, entry := range in.Entries {
		entry.Quote = cloneCostQuote(entry.Quote)
		out.Entries[key] = entry
	}
	return out
}

func cloneCostQuote(in billing.CostQuote) billing.CostQuote {
	out := in
	out.OriginalTotals = append([]billing.Money(nil), in.OriginalTotals...)
	if in.Selected != nil {
		selected := *in.Selected
		out.Selected = &selected
	}
	if in.Valuations != nil {
		out.Valuations = make(map[string]billing.Valuation, len(in.Valuations))
		for currency, valuation := range in.Valuations {
			if valuation.Rate != nil {
				rate := *valuation.Rate
				valuation.Rate = &rate
			}
			out.Valuations[currency] = valuation
		}
	}
	return out
}
