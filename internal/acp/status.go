package acp

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/usageledger"
)

const (
	reasonixStatusSchemaVersion = 1
	sessionStatusMethod         = "_reasonix.io/session/status"
	sessionStatusUpdateMethod   = "_reasonix.io/session/status_update"
)

// ReasonixSchemaCapability advertises one versioned vendor extension in
// agentCapabilities._meta. The method name is the map key so clients can fail
// closed before opening a session.
type ReasonixSchemaCapability struct {
	SchemaVersion int `json:"schemaVersion"`
}

// SessionStatusParams addresses one live ACP session.
type SessionStatusParams struct {
	SessionID string `json:"sessionId"`
}

// SessionSandboxState is the effective sandbox, after all CLI hard overrides
// have been applied. It deliberately reports no credentials or environment.
type SessionSandboxState struct {
	Mode           string   `json:"mode"`
	Engine         string   `json:"engine"`
	Available      bool     `json:"available"`
	WorkspaceRoot  string   `json:"workspaceRoot"`
	WriteRoots     []string `json:"writeRoots"`
	NetworkEnabled bool     `json:"networkEnabled"`
}

// SessionRuntimeState is supplied by the composition root because only it can
// truthfully report config-derived sandbox and planner state.
type SessionRuntimeState struct {
	PlannerMode string              `json:"plannerMode"`
	Sandbox     SessionSandboxState `json:"sandbox"`
}

// SessionRuntimeStateParams identifies the exact controller configuration whose
// effective policy is being reported. Model/profile switches rebuild the
// controller, so status must be recomputed from the same resolved inputs.
type SessionRuntimeStateParams struct {
	Cwd            string
	Model          string
	RuntimeProfile string
}

// SessionRuntimeStateProvider exposes effective process/session policy without
// coupling the ACP adapter to Reasonix configuration internals.
type SessionRuntimeStateProvider interface {
	SessionRuntimeState(ctx context.Context, p SessionRuntimeStateParams) (SessionRuntimeState, error)
}

type ReasonixStatusGoal struct {
	Status    string `json:"status"`
	Objective string `json:"objective,omitempty"`
	// Runtime is the optional Goal usage/runtime summary; absent for old
	// hosts or when no goal is active.
	Runtime *ReasonixGoalRuntime `json:"runtime,omitempty"`
}

type ReasonixGoalRuntime struct {
	TurnsUsed        int    `json:"turnsUsed"`
	TurnsLimit       int    `json:"turnsLimit"` // Deprecated: always 0.
	TokensUsed       int    `json:"tokensUsed"`
	RequestsUsed     int    `json:"requestsUsed,omitempty"`
	WorkDurationMs   int64  `json:"workDurationMs,omitempty"`
	TokensLimit      int    `json:"tokensLimit"` // Deprecated: always 0; retained for protocol compatibility.
	NoProgressTurns  int    `json:"noProgressTurns"`
	NoProgressLimit  int    `json:"noProgressLimit"` // Deprecated: always 0.
	LastReason       string `json:"lastReason,omitempty"`
	StopCause        string `json:"stopCause,omitempty"`
	BudgetExtensions int    `json:"budgetExtensions"` // Deprecated: always 0.
}

type ReasonixTurnOutcome struct {
	Diagnostic *provider.FailureDiagnostic `json:"diagnostic,omitempty"`
	Kind       string                      `json:"kind"`
	Reason     string                      `json:"reason,omitempty"`
}

type ReasonixFinalReadiness struct {
	ReadyForReview bool     `json:"readyForReview"`
	Summary        string   `json:"summary"`
	Risks          []string `json:"risks"`
}

// ReasonixSessionStatus is the stable schemaVersion=1 recovery snapshot.
// Reasoning text and unbounded terminal output are intentionally absent.
type ReasonixSessionStatus struct {
	ProtocolRecovery *provider.ProtocolRecoveryAction `json:"protocolRecovery,omitempty"`
	SchemaVersion    int                              `json:"schemaVersion"`
	Sequence         uint64                           `json:"sequence"`
	SessionID        string                           `json:"sessionId"`
	State            string                           `json:"state"`
	Model            string                           `json:"model"`
	Effort           string                           `json:"effort"`
	Mode             string                           `json:"mode"`
	WorkMode         string                           `json:"workMode"`
	PlannerMode      string                           `json:"plannerMode"`
	Goal             ReasonixStatusGoal               `json:"goal"`
	Phase            string                           `json:"phase"`
	TurnOutcome      ReasonixTurnOutcome              `json:"turnOutcome"`
	FinalReadiness   ReasonixFinalReadiness           `json:"finalReadiness"`
	Sandbox          SessionSandboxState              `json:"sandbox"`
	Usage            ReasonixStatusUsage              `json:"usage"`
}

type ReasonixStatusUpdate struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Sequence      uint64                `json:"sequence"`
	SessionID     string                `json:"sessionId"`
	Event         string                `json:"event"`
	Status        ReasonixSessionStatus `json:"status"`
}

type usageAccumulator struct {
	ledger *usageledger.Ledger
}

func (a *usageAccumulator) owner() *usageledger.Ledger {
	if a.ledger == nil {
		a.ledger = usageledger.New()
	}
	return a.ledger
}

func (a *usageAccumulator) add(e event.Event) {
	a.owner().Add(e)
}

func (a usageAccumulator) wire() ReasonixUsage {
	projection := usageledger.New().Projection()
	if a.ledger != nil {
		projection = a.ledger.Projection()
	}
	usage := ReasonixUsage{
		TotalTokens:      projection.Usage.InputTokens + projection.Usage.OutputTokens,
		PromptTokens:     projection.Usage.InputTokens,
		CompletionTokens: projection.Usage.OutputTokens,
		ReasoningTokens:  projection.Usage.ReasoningTokens,
		CacheHitTokens:   projection.Usage.CacheReadInputTokens,
		CacheMissTokens:  projection.Usage.CacheCreationInputTokens,
		Estimated:        projection.Usage.Estimated,
		UsageSource:      projectionSource(projection),
	}
	if total := usage.CacheHitTokens + usage.CacheMissTokens; total > 0 {
		ratio := float64(usage.CacheHitTokens) / float64(total)
		usage.CacheHitRatio = &ratio
	}
	if cost := projection.Cost; cost.CostQuote != nil {
		usage.CostQuote = cost.CostQuote
		costComplete := cost.CostComplete
		displayComplete := cost.DisplayComplete
		usage.CostComplete = &costComplete
		usage.DisplayComplete = &displayComplete
		usage.DisplayStatus = cost.DisplayStatus
		usage.AggregateMode = cost.AggregateMode
		usage.OriginalTotals = append([]billing.Money(nil), cost.OriginalTotals...)
		if cost.TotalCost != nil && cost.Currency != "" {
			usage.EstimatedCost = cost.TotalCost
			currency := cost.Currency
			usage.Currency = &currency
		}
	}
	return usage
}

func projectionSource(projection usageledger.Projection) string {
	source := ""
	for _, row := range projection.ModelUsage {
		if source == "" {
			source = row.Source
		} else if source != row.Source {
			return "mixed"
		}
	}
	if source == "" {
		return event.UsageSourceExecutor
	}
	return source
}

type statusTelemetry struct {
	mu             sync.Mutex
	sequence       uint64
	state          string
	phase          string
	turnOutcome    ReasonixTurnOutcome
	finalReadiness ReasonixFinalReadiness
	turnUsage      usageAccumulator
	cumulative     usageAccumulator
	goalOverride   string
}

func newStatusTelemetry() *statusTelemetry {
	return &statusTelemetry{
		state:       "idle",
		phase:       "idle",
		turnOutcome: ReasonixTurnOutcome{Kind: "none"},
		finalReadiness: ReasonixFinalReadiness{
			Risks: []string{},
		},
	}
}

func (t *statusTelemetry) mutate(fn func(*statusTelemetry)) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	fn(t)
	t.sequence++
	return t.sequence
}

func (t *statusTelemetry) beginTurn() {
	t.mutate(func(t *statusTelemetry) {
		t.state = "running"
		t.phase = "starting"
		t.turnOutcome = ReasonixTurnOutcome{Kind: "none"}
		t.finalReadiness = ReasonixFinalReadiness{Risks: []string{}}
		t.turnUsage = usageAccumulator{}
		t.goalOverride = ""
	})
}

func (t *statusTelemetry) onEvent(e event.Event) (string, bool) {
	switch e.Kind {
	case event.Phase:
		t.mutate(func(t *statusTelemetry) {
			t.phase = normalizeStatusPhase(e)
		})
		return "phase", true
	case event.Usage:
		t.mutate(func(t *statusTelemetry) {
			t.turnUsage.add(e)
			t.cumulative.add(e)
		})
		return "usage", true
	case event.ApprovalRequest:
		t.mutate(func(t *statusTelemetry) { t.phase = "waiting_permission" })
		return "phase", true
	case event.AskRequest:
		t.mutate(func(t *statusTelemetry) { t.phase = "waiting_input" })
		return "phase", true
	case event.ToolDispatch:
		t.mutate(func(t *statusTelemetry) { t.phase = "implementing" })
	case event.Notice:
		if e.Code == event.NoticeCodeFinalReadiness {
			t.mutate(func(t *statusTelemetry) { t.phase = "checking_readiness" })
			return "phase", true
		}
	}
	return "", false
}

func (t *statusTelemetry) finishTurn(runErr error, cancelled bool, goalStatus, summary string) string {
	eventName := "completion"
	t.mutate(func(t *statusTelemetry) {
		t.state = "idle"
		t.finalReadiness.Summary = clipStatusText(summary, 16_384)
		t.finalReadiness.Risks = []string{}
		t.goalOverride = ""
		switch {
		case cancelled:
			t.phase = "cancelled"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "cancelled"}
			t.goalOverride = "cancelled"
			eventName = "completion"
		case runErr == nil && goalStatus == control.GoalStatusComplete:
			t.phase = "review_ready"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "completed"}
			t.finalReadiness.ReadyForReview = true
		case runErr == nil && (goalStatus == "" || goalStatus == control.GoalStatusStopped):
			t.phase = "completed"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "completed"}
			t.finalReadiness.ReadyForReview = true
		case goalStatus == control.GoalStatusBlocked:
			t.phase = "paused"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: "goal blocked"}
			eventName = "pause"
		default:
			var readinessErr *agent.FinalReadinessError
			var recoveryPause *agent.RecoveryPauseError
			var completionPause *agent.CompletionUncertainError
			_, runPause := agent.InspectRunPause(runErr)
			switch {
			case errors.As(runErr, &readinessErr):
				t.phase = "readiness_paused"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: clipStatusError(readinessErr, 2_048)}
				t.finalReadiness.Risks = redactStatusTexts(readinessErr.Missing, 2_048)
				eventName = "pause"
			case errors.As(runErr, &recoveryPause):
				t.phase = "recovery_paused"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: clipStatusText(recoveryPause.Error(), 2_048)}
				eventName = "pause"
			case errors.As(runErr, &completionPause):
				t.phase = "completion_uncertain"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: clipStatusText(completionPause.Error(), 2_048)}
				eventName = "pause"
			case runPause:
				t.phase = "paused"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: clipStatusError(runErr, 2_048)}
				eventName = "pause"
			case runErr != nil:
				t.phase = "error"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "error", Reason: clipStatusError(runErr, 2_048), Diagnostic: provider.DiagnoseFailure(runErr)}
				t.goalOverride = "failed"
				eventName = "error"
			default:
				t.phase = "paused"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: "goal is not complete"}
				eventName = "pause"
			}
		}
	})
	return eventName
}

type statusTelemetrySnapshot struct {
	sequence       uint64
	state          string
	phase          string
	turnOutcome    ReasonixTurnOutcome
	finalReadiness ReasonixFinalReadiness
	turnUsage      ReasonixUsage
	cumulative     ReasonixUsage
	goalOverride   string
}

type persistedUsageAccumulator struct {
	PromptTokens     int                   `json:"promptTokens"`
	CompletionTokens int                   `json:"completionTokens"`
	ReasoningTokens  int                   `json:"reasoningTokens"`
	CacheHitTokens   int                   `json:"cacheHitTokens"`
	CacheMissTokens  int                   `json:"cacheMissTokens"`
	Estimated        bool                  `json:"estimated,omitempty"`
	Events           int                   `json:"events"`
	PricedEvents     int                   `json:"pricedEvents"`
	EstimatedCost    float64               `json:"estimatedCost"`
	Currency         string                `json:"currency,omitempty"`
	Source           string                `json:"source,omitempty"`
	CostComplete     *bool                 `json:"costComplete,omitempty"`
	Owner            *usageledger.Snapshot `json:"owner,omitempty"`
}

type persistedStatusTelemetry struct {
	Sequence       uint64                    `json:"sequence"`
	State          string                    `json:"state"`
	Phase          string                    `json:"phase"`
	TurnOutcome    ReasonixTurnOutcome       `json:"turnOutcome"`
	FinalReadiness ReasonixFinalReadiness    `json:"finalReadiness"`
	TurnUsage      persistedUsageAccumulator `json:"turnUsage"`
	Cumulative     persistedUsageAccumulator `json:"cumulative"`
	GoalOverride   string                    `json:"goalOverride,omitempty"`
}

func persistUsage(a usageAccumulator) persistedUsageAccumulator {
	snapshot := usageledger.New().Snapshot()
	var owner *usageledger.Snapshot
	if a.ledger != nil {
		snapshot = a.ledger.Snapshot()
		owner = &snapshot
	}
	wire := a.wire()
	var estimatedCost float64
	if wire.EstimatedCost != nil {
		estimatedCost = *wire.EstimatedCost
	}
	var currency string
	if wire.Currency != nil {
		currency = *wire.Currency
	}
	return persistedUsageAccumulator{
		PromptTokens: wire.PromptTokens, CompletionTokens: wire.CompletionTokens,
		ReasoningTokens: wire.ReasoningTokens, CacheHitTokens: wire.CacheHitTokens,
		CacheMissTokens: wire.CacheMissTokens, Estimated: wire.Estimated, Events: snapshot.UsageEvents,
		PricedEvents: snapshot.QuoteEvents, EstimatedCost: estimatedCost,
		Currency: currency, Source: wire.UsageSource, CostComplete: wire.CostComplete,
		Owner: owner,
	}
}

func restoreUsage(a persistedUsageAccumulator) usageAccumulator {
	if a.Owner != nil {
		return usageAccumulator{ledger: usageledger.Restore(*a.Owner)}
	}
	tokens := usageledger.Tokens{
		InputTokens: a.PromptTokens, OutputTokens: a.CompletionTokens,
		ReasoningTokens: a.ReasoningTokens, CacheReadInputTokens: a.CacheHitTokens,
		CacheCreationInputTokens: a.CacheMissTokens, Estimated: a.Estimated,
	}
	events := a.Events
	if events == 0 && (tokens.InputTokens != 0 || tokens.OutputTokens != 0 || tokens.ReasoningTokens != 0 || tokens.CacheReadInputTokens != 0 || tokens.CacheCreationInputTokens != 0) {
		events = 1
	}
	source := strings.TrimSpace(a.Source)
	if source == "" {
		source = event.UsageSourceExecutor
	}
	snapshot := usageledger.Snapshot{
		Usage: tokens, Rows: map[string]usageledger.RowSnapshot{},
		QuoteLedger: billing.NewLedger(), UsageEvents: events,
	}
	if events > 0 {
		snapshot.Rows[source+":"] = usageledger.RowSnapshot{
			Source: source, Usage: tokens, UsageEvents: events,
		}
		snapshot.IncompleteReasons = []string{"missing_model_attribution"}
	}
	pricedEvents := min(max(a.PricedEvents, 0), events)
	if pricedEvents > 0 && billing.NormalizeCurrency(a.Currency) != "" {
		complete := pricedEvents == events
		if a.CostComplete != nil {
			complete = *a.CostComplete
		}
		money := billing.MoneyOf(billing.NewAmountFromFloat(a.EstimatedCost), a.Currency)
		selected := money
		status := billing.DisplayStatusUnavailable
		if complete {
			status = billing.DisplayStatusMatched
		}
		quote := billing.CostQuote{
			Original: money, Selected: &selected, Estimated: true,
			CostComplete: complete, DisplayComplete: complete, Complete: complete,
			DisplayStatus: status, AggregateMode: billing.AggregateModeSingleCurrency,
			UsageSource: source, PricingFingerprint: "legacy-acp-status",
			LegacyEstimate: true,
		}
		snapshot.QuoteLedger.Add(quote, billing.UsageTokens{
			PromptTokens: a.PromptTokens, CompletionTokens: a.CompletionTokens,
			CacheHitTokens: a.CacheHitTokens, CacheMissTokens: a.CacheMissTokens,
			Estimated: a.Estimated,
		}, time.Time{})
		snapshot.QuoteEvents = pricedEvents
		row := snapshot.Rows[source+":"]
		row.QuoteEvents = pricedEvents
		snapshot.Rows[source+":"] = row
	}
	return usageAccumulator{ledger: usageledger.Restore(snapshot)}
}

func (t *statusTelemetry) persisted() *persistedStatusTelemetry {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return &persistedStatusTelemetry{
		Sequence: t.sequence, State: t.state, Phase: t.phase,
		TurnOutcome: redactTurnOutcome(t.turnOutcome),
		FinalReadiness: ReasonixFinalReadiness{
			ReadyForReview: t.finalReadiness.ReadyForReview,
			Summary:        clipStatusText(t.finalReadiness.Summary, 16_384),
			Risks:          redactStatusTexts(t.finalReadiness.Risks, 2_048),
		},
		TurnUsage: persistUsage(t.turnUsage), Cumulative: persistUsage(t.cumulative),
		GoalOverride: t.goalOverride,
	}
}

func restoreStatusTelemetry(saved *persistedStatusTelemetry) *statusTelemetry {
	t := newStatusTelemetry()
	if saved == nil {
		return t
	}
	interrupted := saved.State == "running"
	t.sequence = saved.Sequence
	// A restored process never owns the turn that wrote a running snapshot.
	// Publish a new, terminal recovery state instead of leaving supervisors
	// waiting on work that no longer exists in this runtime.
	t.state = "idle"
	t.phase = normalizePersistedStatusPhase(saved.Phase)
	t.turnOutcome = redactTurnOutcome(saved.TurnOutcome)
	if t.turnOutcome.Kind == "" {
		t.turnOutcome.Kind = "none"
	}
	t.finalReadiness = ReasonixFinalReadiness{
		ReadyForReview: saved.FinalReadiness.ReadyForReview,
		Summary:        clipStatusText(saved.FinalReadiness.Summary, 16_384),
		Risks:          redactStatusTexts(saved.FinalReadiness.Risks, 2_048),
	}
	t.turnUsage = restoreUsage(saved.TurnUsage)
	t.cumulative = restoreUsage(saved.Cumulative)
	t.goalOverride = saved.GoalOverride
	if interrupted {
		t.sequence++
		t.phase = "recovery_paused"
		t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: "previous turn interrupted"}
		t.finalReadiness.ReadyForReview = false
	}
	return t
}

func (t *statusTelemetry) snapshot() statusTelemetrySnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return statusTelemetrySnapshot{
		sequence: t.sequence,
		state:    t.state,
		phase:    t.phase,
		turnOutcome: ReasonixTurnOutcome{
			Kind: t.turnOutcome.Kind, Reason: clipStatusCredentialText(t.turnOutcome.Reason, 2_048), Diagnostic: t.turnOutcome.Diagnostic,
		},
		finalReadiness: ReasonixFinalReadiness{
			ReadyForReview: t.finalReadiness.ReadyForReview,
			Summary:        clipStatusText(t.finalReadiness.Summary, 16_384),
			Risks:          redactStatusTexts(t.finalReadiness.Risks, 2_048),
		},
		turnUsage:    t.turnUsage.wire(),
		cumulative:   t.cumulative.wire(),
		goalOverride: t.goalOverride,
	}
}

func redactTurnOutcome(outcome ReasonixTurnOutcome) ReasonixTurnOutcome {
	outcome.Reason = clipStatusCredentialText(outcome.Reason, 2_048)
	return outcome
}

func defaultSessionRuntimeState(cwd string) SessionRuntimeState {
	engine := "bubblewrap"
	if runtime.GOOS == "darwin" {
		engine = "seatbelt"
	}
	return SessionRuntimeState{
		PlannerMode: "on",
		Sandbox: SessionSandboxState{
			Mode:          "enforce",
			Engine:        engine,
			Available:     true,
			WorkspaceRoot: cwd,
			WriteRoots:    []string{cwd},
		},
	}
}

func normalizeStatusEffort(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "auto"
	}
	return strings.TrimSpace(*value)
}

func normalizeGoalStatus(value string) string {
	switch value {
	case control.GoalStatusRunning, control.GoalStatusComplete, control.GoalStatusBlocked:
		return value
	default:
		return "none"
	}
}

func redactStatusTexts(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, clipStatusText(value, limit))
	}
	return out
}

func normalizeStatusPhase(e event.Event) string {
	switch strings.TrimSpace(e.Source) {
	case event.UsageSourcePlanner:
		return "planning"
	case event.UsageSourceExecutor:
		return "implementing"
	}
	lower := strings.ToLower(strings.TrimSpace(e.Text))
	switch {
	case strings.Contains(lower, "planning"):
		return "planning"
	case strings.Contains(lower, "executing"), strings.Contains(lower, "implementing"):
		return "implementing"
	default:
		return "working"
	}
}

func normalizePersistedStatusPhase(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "idle", "starting", "planning", "working", "implementing",
		"waiting_permission", "waiting_input", "checking_readiness",
		"cancelled", "review_ready", "completed", "paused",
		"readiness_paused", "recovery_paused", "error":
		return value
	default:
		return normalizeStatusPhase(event.Event{Kind: event.Phase, Text: value})
	}
}

func (s *service) sessionRuntimeState(ctx context.Context, p SessionRuntimeStateParams) (SessionRuntimeState, error) {
	if provider, ok := s.factory.(SessionRuntimeStateProvider); ok {
		state, err := provider.SessionRuntimeState(ctx, p)
		if err != nil {
			return SessionRuntimeState{}, err
		}
		if strings.TrimSpace(state.PlannerMode) == "" {
			state.PlannerMode = "on"
		}
		if state.Sandbox.WriteRoots == nil {
			state.Sandbox.WriteRoots = []string{}
		}
		return state, nil
	}
	return defaultSessionRuntimeState(p.Cwd), nil
}

func (s *service) bindStatusEvents(sess *acpSession) {
	if sess == nil || sess.sink == nil {
		return
	}
	if sess.status == nil {
		sess.status = newStatusTelemetry()
	}
	sess.sink.bindStatus(func(e event.Event) {
		eventName, publish := sess.status.onEvent(e)
		if publish {
			s.publishStatus(sess, eventName)
		}
	})
}

func (s *service) sessionStatus(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionStatusParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionStatusMethod + ": " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionStatusMethod + ": unknown session " + p.SessionID}
	}
	return sess.statusSnapshot(), nil
}

func (s *service) publishStatus(sess *acpSession, eventName string) {
	if sess == nil {
		return
	}
	status := sess.statusSnapshot()
	_ = s.conn.Notify(sessionStatusUpdateMethod, ReasonixStatusUpdate{
		SchemaVersion: reasonixStatusSchemaVersion,
		Sequence:      status.Sequence,
		SessionID:     status.SessionID,
		Event:         eventName,
		Status:        status,
	})
}

func (s *acpSession) statusSnapshot() ReasonixSessionStatus {
	s.mu.Lock()
	id := s.id
	ctrl := s.ctrl
	model := s.model
	effort := cloneStringPtr(s.effortOverride)
	mode := s.modeID
	runtimeState := s.runtimeState
	telemetry := s.status
	s.mu.Unlock()

	if telemetry == nil {
		telemetry = newStatusTelemetry()
	}
	t := telemetry.snapshot()
	goalStatus := "none"
	goalObjective := ""
	var goalRuntime *ReasonixGoalRuntime
	if ctrl != nil {
		goalStatus = normalizeGoalStatus(ctrl.GoalStatus())
		goalObjective = clipStatusText(ctrl.Goal(), 16_384)
		if strings.TrimSpace(goalObjective) != "" {
			rt := ctrl.GoalRuntime()
			goalRuntime = &ReasonixGoalRuntime{
				TurnsUsed:        rt.TurnsUsed,
				TurnsLimit:       rt.TurnsLimit,
				TokensUsed:       rt.TokensUsed,
				RequestsUsed:     rt.RequestsUsed,
				WorkDurationMs:   rt.WorkDurationMs,
				TokensLimit:      rt.TokensLimit,
				NoProgressTurns:  rt.NoProgressTurns,
				NoProgressLimit:  rt.NoProgressLimit,
				LastReason:       rt.LastReason,
				StopCause:        rt.StopCause,
				BudgetExtensions: rt.BudgetExtensions,
			}
		}
	}
	if t.goalOverride != "" {
		goalStatus = t.goalOverride
	}
	mode = normalizeACPCollaborationMode(mode)
	// WorkMode is a deprecated wire-compat field pinned to the historical
	// default; execution modes no longer exist at runtime.
	workMode := "balanced"
	if runtimeState.PlannerMode != "off" {
		runtimeState.PlannerMode = "on"
	}
	if runtimeState.Sandbox.WriteRoots == nil {
		runtimeState.Sandbox.WriteRoots = []string{}
	}
	phase := strings.TrimSpace(t.phase)
	if phase == "" {
		phase = "idle"
	}
	state := t.state
	if state != "running" {
		state = "idle"
	}
	var protocolRecovery *provider.ProtocolRecoveryAction
	if pending, ok := ctrl.(interface {
		PendingProtocolRecovery() *provider.ProtocolRecoveryAction
	}); ok {
		protocolRecovery = pending.PendingProtocolRecovery()
	}
	return ReasonixSessionStatus{
		ProtocolRecovery: protocolRecovery,
		SchemaVersion:    reasonixStatusSchemaVersion,
		Sequence:         t.sequence,
		SessionID:        id,
		State:            state,
		Model:            strings.TrimSpace(model),
		Effort:           normalizeStatusEffort(effort),
		Mode:             mode,
		WorkMode:         workMode,
		PlannerMode:      runtimeState.PlannerMode,
		Goal: ReasonixStatusGoal{
			Status:    goalStatus,
			Objective: goalObjective,
			Runtime:   goalRuntime,
		},
		Phase:          phase,
		TurnOutcome:    t.turnOutcome,
		FinalReadiness: t.finalReadiness,
		Sandbox:        runtimeState.Sandbox,
		Usage: ReasonixStatusUsage{
			Turn:       t.turnUsage,
			Cumulative: t.cumulative,
		},
	}
}

func finalAssistantSummary(ctrl acpController) string {
	if ctrl == nil {
		return ""
	}
	history := ctrl.History()
	for _, v := range slices.Backward(history) {
		if v.Role == provider.RoleAssistant && strings.TrimSpace(v.Content) != "" {
			return v.Content
		}
	}
	return ""
}
