package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/billing"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

const (
	statusFooterIndent         = "  "
	statusFooterGroupGap       = 2
	statusFooterInlineModelMin = 24
)

func footerLabel(label string) string {
	return themeFg(activeCLITheme.subtle, label)
}

func footerHint(hint string) string {
	return themeFg(activeCLITheme.subtle, hint)
}

func footerValue(value string) string {
	return themeFg(activeCLITheme.muted, value)
}

func footerInfo(value string) string {
	return themeFg(activeCLITheme.info, value)
}

func footerMetric(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return footerLabel(label) + " " + value
}

// renderTurnReceipt attaches the completed turn's token and cost breakdown to
// the assistant response. Unlike the persistent footer, this is historical
// message metadata: it stays in transcript scrollback and deliberately uses a
// quieter palette than runtime/session state.
func renderTurnReceipt(u *provider.Usage, p *provider.Pricing, d *event.CacheDiagnostics) string {
	var quote *billing.CostQuote
	if u != nil && p != nil {
		quote = event.EnsureCostQuote(event.Event{Kind: event.Usage, Usage: u, Pricing: p}, nil)
	}
	return renderQuotedTurnReceipt(u, quote, d)
}

func renderQuotedTurnReceipt(u *provider.Usage, q *billing.CostQuote, d *event.CacheDiagnostics) string {
	if u == nil || u.TotalTokens == 0 {
		return ""
	}

	total := shortTokens(u.TotalTokens) + " tok"
	if u.Estimated {
		total = "≈" + total
	}
	groups := []string{total}
	if u.PromptTokens > 0 {
		cached := u.CacheHitTokens
		fresh := u.CacheMissTokens
		if fresh == 0 {
			fresh = max(u.PromptTokens-cached, 0)
		}
		groups = append(groups,
			"in "+shortTokens(u.PromptTokens),
			"cached "+shortTokens(cached),
			"new "+shortTokens(fresh),
		)
	}
	groups = append(groups, "out "+shortTokens(u.CompletionTokens))
	if u.ReasoningTokens > 0 {
		groups = append(groups, "reasoning "+shortTokens(u.ReasoningTokens))
	}
	if q != nil && q.CostComplete {
		// Host quotes are estimates; never present a bare zero as real spend.
		money := q.Original
		if q.Selected != nil {
			money = *q.Selected
		}
		cost := money.Float64()
		if cost > 0 {
			groups = append(groups, fmt.Sprintf("≈%s%.4f", billing.CurrencySymbol(money.Currency), cost))
		} else {
			groups = append(groups, "cost n/a")
		}
		if band := localizedRateBand(q.RateBand); band != "" {
			groups = append(groups, band)
		}
	}
	if u.Estimated {
		groups = append(groups, "estimated")
	}

	separator := footerHint(" · ")
	styled := make([]string, 0, len(groups))
	for _, group := range groups {
		styled = append(styled, footerValue(group))
	}
	receipt := statusFooterIndent + footerLabel(i18n.M.ChatTurnReceiptLabel) + "  " + strings.Join(styled, separator)
	if d != nil && d.PrefixChanged {
		reasons := strings.Join(d.PrefixChangeReasons, "+")
		if reasons == "" {
			reasons = "unknown"
		}
		receipt += separator + themeFg(activeCLITheme.warn, "cache prefix changed: "+reasons)
	}
	return receipt
}

func localizedRateBand(band string) string {
	switch band {
	case billing.RateBandPeak:
		return i18n.M.RateBandPeak
	case billing.RateBandOffPeak:
		return i18n.M.RateBandOffPeak
	case billing.RateBandMixed:
		return i18n.M.RateBandMixed
	default:
		return ""
	}
}

func mergeSessionRateBand(current *billing.CostQuote, next billing.CostQuote) string {
	if current == nil {
		return next.RateBand
	}
	if current.RateBand == "" || next.RateBand == "" {
		return ""
	}
	if current.RateBand == billing.RateBandMixed || next.RateBand == billing.RateBandMixed || current.RateBand != next.RateBand {
		return billing.RateBandMixed
	}
	return current.RateBand
}

func (m *chatTUI) addSessionCostQuote(next *billing.CostQuote) {
	if m == nil || next == nil {
		return
	}
	band := mergeSessionRateBand(m.sessionCostQuote, *next)
	quotes := []billing.CostQuote{*next}
	if m.sessionCostQuote != nil {
		quotes = append([]billing.CostQuote{*m.sessionCostQuote}, quotes...)
	}
	display := ""
	if next.Selected != nil {
		display = next.Selected.Currency
	}
	total := billing.AggregateQuotes(quotes, display)
	total.RateBand = band
	total.RatedAt = ""
	m.sessionCostQuote = &total
}

func (m chatTUI) sessionCostStatus() string {
	q := m.sessionCostQuote
	if q == nil || !q.CostComplete {
		return ""
	}
	money := q.Original
	if q.Selected != nil {
		money = *q.Selected
	}
	if money.Float64() <= 0 {
		return ""
	}
	value := fmt.Sprintf("≈%s%.4f", billing.CurrencySymbol(money.Currency), money.Float64())
	if band := localizedRateBand(q.RateBand); band != "" {
		value += " · " + band
	}
	return value
}

// primaryStatusLine renders the interaction half of the first footer row. The
// model/profile group is laid out separately so it can stay right-anchored on
// wide terminals and move as one unit on narrow terminals.
func (m chatTUI) primaryStatusLine(modeTag string, shellMode, cancelRequested bool) string {
	status := statusFooterIndent + modeTag
	if state := m.statusStateText(shellMode, cancelRequested); state != "" {
		status += " · " + footerValue(ansi.Strip(state))
	}
	if mt := m.mouseTag(); mt != "" {
		status += " · " + mt
	}
	return status
}

// statusProjection is the single footer layout result consumed by both View
// and viewport sizing, so rendered rows and reserved rows cannot diverge.
type statusProjection struct {
	primary string
	working string
	block   string
	rows    int
}

func (m chatTUI) projectStatus(width int, styled bool) statusProjection {
	if m.ctrl == nil {
		return statusProjection{rows: 3}
	}
	shellMode := strings.HasPrefix(strings.TrimSpace(m.input.Value()), "!")
	cancelRequested := m.cancelRequested()
	modeTag := m.projectStatusModeTag(shellMode, styled)
	p := statusProjection{
		primary: m.primaryStatusLine(modeTag, shellMode, cancelRequested),
	}
	if m.state == tuiRunning {
		p.working = m.runningWorkingLine(cancelRequested, styled)
	}
	p.block = m.renderStatusBlock(p.primary, width)
	if p.working != "" {
		p.rows += strings.Count(wrapStatusLine(p.working, width), "\n") + 1
	}
	p.rows += strings.Count(p.block, "\n") + 1
	return p
}

func (m chatTUI) projectStatusModeTag(shellMode, styled bool) string {
	label := m.modeTagText()
	background := statusAutoColor
	foreground := modeTagDark
	switch {
	case shellMode:
		label = "Shell"
		background = statusShellColor
		foreground = modeTagLight
	case m.ctrl.AutoApproveTools():
		background = statusYoloColor
		foreground = modeTagLight
	case m.planMode:
		background = statusPlanColor
		foreground = modeTagLight
	}
	if !styled {
		return " " + label + " "
	}
	return modeTagStyle(background, foreground).Render(label)
}

// presetTag mirrors the desktop's preset chips in the status line: the default
// standard posture stays quiet, delivery is always visible so a /preset switch
// reads back from the UI.
func (m chatTUI) presetTag() string {
	if m.ctrl == nil || m.ctrl.QualityFloor() != control.QualityFloorDelivery {
		return ""
	}
	value := themeStyle(activeCLITheme.info).Bold(true).Render(control.QualityFloorDelivery)
	return footerMetric(i18n.M.ChatStatusPresetLabel, value)
}

// statusModelWorkGroup is the bounded, session-level group placed at the right
// edge of the first footer row. A custom statusline still replaces every
// built-in data field, matching its existing configuration contract.
func (m chatTUI) statusModelWorkGroup(maxWidth int) string {
	if m.statuslineCmd != "" && m.statuslineOut != "" {
		return ""
	}
	if maxWidth <= 0 {
		maxWidth = 1
	}
	model, effort, preset := m.modelComboTag(), m.effortTag(), m.presetTag()
	fields := make([]string, 0, 3)
	for _, field := range []string{model, effort, preset} {
		if field != "" {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return ""
	}
	full := strings.Join(fields, " · ")
	if visibleWidth(full) <= maxWidth {
		return full
	}
	// Vision is a short-lived phase. Preserve executor, vision, planner and an
	// explicit preset before spending a third footer row on default effort.
	if m.visionTag() != "" {
		essential := strings.Join(nonEmptyStrings([]string{model, preset}), " · ")
		if visibleWidth(essential) <= maxWidth {
			return essential
		}
	}
	return footerHint(compactMiddle(ansi.Strip(full), maxWidth))
}

func cacheStatusColor(rate float64) cliColor {
	switch {
	case rate >= 80:
		return activeCLITheme.success
	case rate >= 50:
		return activeCLITheme.info
	default:
		return activeCLITheme.warn
	}
}

func renderContextStatusGroups(used, window int, ratio float64) []string {
	if used == 0 || window == 0 {
		return nil
	}
	pct := used * 100 / window
	ctxValue := fmt.Sprintf("%s (%d%%)", shortTokens(used), pct)

	if ratio <= 0 || ratio >= 1 {
		ctxValue = fmt.Sprintf("%s / %s (%d%%)", shortTokens(used), shortTokens(window), pct)
		color := activeCLITheme.muted
		switch {
		case pct >= 85:
			color = activeCLITheme.danger
		case pct >= 60:
			color = activeCLITheme.warn
		}
		return []string{footerMetric(i18n.M.ChatStatusContextLabel, themeFg(color, ctxValue))}
	}

	threshold := int(ratio * 100)
	left := max(threshold-pct, 0)
	ctxColor := activeCLITheme.muted
	compactColor := activeCLITheme.muted
	switch {
	case pct >= threshold:
		// Preserve two levels of urgency from the selected design: context is a
		// warning, while the exhausted compaction headroom is the actual danger.
		ctxColor = activeCLITheme.warn
		compactColor = activeCLITheme.danger
	case left <= 10:
		ctxColor = activeCLITheme.warn
		compactColor = activeCLITheme.warn
	}
	return []string{
		footerMetric(i18n.M.ChatStatusContextLabel, themeFg(ctxColor, ctxValue)),
		footerMetric(i18n.M.ChatStatusCompactLabel, themeFg(compactColor, fmt.Sprintf("%d%%", left))),
	}
}

// statusTelemetryGroups returns independently placeable session metrics. Git is
// intentionally excluded because it owns the flexible identity slot; keeping
// metrics separate lets narrow layouts wrap only between semantic groups.
func (m chatTUI) statusTelemetryGroups() []string {
	if m.statuslineCmd != "" && m.statuslineOut != "" {
		return []string{m.statuslineOut}
	}
	var data []string
	if m.ctrl != nil {
		if body, rate, ok := m.cacheStatus(); ok && m.presentation.StatusCache {
			data = append(data, themeFg(cacheStatusColor(rate), body))
		}
		if context := m.contextTag(); context != "" {
			data = append(data, context)
		}
		if jt := m.jobsTag(); jt != "" {
			data = append(data, jt)
		}
	}
	if balance := m.balanceTag(); balance != "" && m.presentation.StatusCost {
		data = append(data, balance)
	}
	if cost := m.sessionCostStatus(); cost != "" {
		data = append(data, footerMetric(i18n.M.ChatStatusCostLabel, footerValue(cost)))
	}
	return data
}

// renderStatusBlock owns the complete persistent footer layout. The optional
// data band is separated from interaction state when Git or telemetry exists;
// narrow screens add deliberate left-aligned rows only between semantic groups.
func (m chatTUI) renderStatusBlock(primary string, width int) string {
	if width <= 0 {
		width = 1
	}
	primary = hideStatusHintWhenKeyNamesCannotFit(primary, width)
	if m.presentation.StatusLayout == "two" {
		first, modelOverflow := m.layoutBoundedPrimaryStatusRow(primary, width)
		second := m.layoutBoundedStatusDataRow(modelOverflow, width)
		if second == "" {
			return first
		}
		if m.presentation.Profile == "hybrid" {
			return statusFooterDivider(width) + "\n" + first + "\n" + second
		}
		return first + "\n" + statusFooterDivider(width) + "\n" + second
	}

	modelWidth := max(width-visibleWidth(statusFooterIndent), 1)
	if inlineWidth := width - visibleWidth(primary) - statusFooterGroupGap; inlineWidth >= statusFooterInlineModelMin {
		modelWidth = inlineWidth
	}
	modelWork := m.statusModelWorkGroup(modelWidth)
	first := layoutStatusSides(primary, modelWork, width)
	second := m.layoutGitTelemetry(width)
	groups := []string{strings.TrimSpace(first), strings.TrimSpace(strings.ReplaceAll(second, "\n", " · "))}
	return wrapStatusGroups(strings.Join(nonEmptyStrings(groups), " · "), width)
}

// layoutBoundedPrimaryStatusRow keeps the interaction and model groups on one
// physical row whenever the model identity remains legible. Any model group
// that cannot fit moves into the single data row owned by layout="two".
func (m chatTUI) layoutBoundedPrimaryStatusRow(primary string, width int) (row, modelOverflow string) {
	primary = singleStatusRow(primary, width)
	available := width - visibleWidth(primary) - statusFooterGroupGap
	if available >= 16 {
		model := m.statusModelWorkGroup(available)
		if model != "" && visibleWidth(model) <= available {
			return primary + strings.Repeat(" ", width-visibleWidth(primary)-visibleWidth(model)) + model, ""
		}
	}
	return primary, m.statusModelWorkGroup(max(width-visibleWidth(statusFooterIndent), 1))
}

func singleStatusRow(value string, width int) string {
	value = strings.ReplaceAll(value, "\n", " · ")
	if visibleWidth(value) <= width {
		return value
	}
	return ansi.Truncate(value, max(width, 1), "…")
}

// layoutBoundedStatusDataRow is the responsive priority policy for the second
// content row. It never wraps. At narrower widths cumulative cost/rate and the
// workspace identity yield before balance, compaction headroom, and cache.
func (m chatTUI) layoutBoundedStatusDataRow(modelOverflow string, width int) string {
	available := max(width-visibleWidth(statusFooterIndent), 1)
	if m.statuslineCmd != "" && m.statuslineOut != "" {
		custom := singleStatusRow(strings.TrimSpace(m.statuslineOut), available)
		identityBudget := available - visibleWidth(custom) - statusFooterGroupGap
		if identity := m.statusIdentitySingleRow(identityBudget); identity != "" {
			padding := available - visibleWidth(identity) - visibleWidth(custom)
			return statusFooterIndent + identity + strings.Repeat(" ", padding) + custom
		}
		return statusFooterIndent + custom
	}

	selected := make(map[int]string)
	used := 0
	add := func(order int, value string) bool {
		if strings.TrimSpace(ansi.Strip(value)) == "" {
			return false
		}
		gap := 0
		if len(selected) > 0 {
			gap = statusFooterGroupGap
		}
		if used+gap+visibleWidth(value) > available {
			return false
		}
		selected[order] = value
		used += gap + visibleWidth(value)
		return true
	}

	// Priority order is intentionally different from display order.
	add(0, modelOverflow)
	if balance := m.balanceTag(); balance != "" && m.presentation.StatusCost {
		add(5, balance)
	}
	if context := m.contextStatusForWidth(width); context != "" {
		add(3, context)
	}
	if body, rate, ok := m.cacheStatus(); ok && m.presentation.StatusCache {
		if width < 64 {
			if _, avg, found := strings.Cut(body, " · "); found {
				body = avg
			}
		}
		add(2, themeFg(cacheStatusColor(rate), body))
	}
	if jobs := m.jobsTag(); jobs != "" {
		add(4, jobs)
	}

	remaining := available - used
	if len(selected) > 0 {
		remaining -= statusFooterGroupGap
	}
	if remaining >= 8 {
		if identity := m.statusIdentitySingleRow(remaining); identity != "" {
			add(1, identity)
		}
	}
	if m.presentation.StatusCost {
		if cost := m.sessionCostStatus(); cost != "" {
			add(6, footerMetric(i18n.M.ChatStatusCostLabel, footerValue(cost)))
		}
	}

	values := make([]string, 0, len(selected))
	for order := 0; order <= 6; order++ {
		if value := selected[order]; value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return ""
	}

	// Keep identity/model spill on the left and telemetry on the right. Besides
	// matching the primary row's model edge, this makes added vision identity
	// consume horizontal space instead of creating another physical row.
	left := nonEmptyStrings([]string{selected[0], selected[1]})
	right := nonEmptyStrings([]string{selected[2], selected[3], selected[4], selected[5], selected[6]})
	if len(left) > 0 && len(right) > 0 {
		leftText := strings.Join(left, strings.Repeat(" ", statusFooterGroupGap))
		rightText := strings.Join(right, strings.Repeat(" ", statusFooterGroupGap))
		padding := available - visibleWidth(leftText) - visibleWidth(rightText)
		if padding >= statusFooterGroupGap {
			return statusFooterIndent + leftText + strings.Repeat(" ", padding) + rightText
		}
	}
	return statusFooterIndent + strings.Join(values, strings.Repeat(" ", statusFooterGroupGap))
}

func (m chatTUI) contextStatusForWidth(width int) string {
	if width >= 96 {
		return m.contextTag()
	}
	if m.ctrl == nil {
		return ""
	}
	used, window := m.ctrl.ContextSnapshot()
	if used <= 0 || window <= 0 {
		return ""
	}
	pct := used * 100 / window
	ratio := m.ctrl.CompactRatio()
	if ratio <= 0 || ratio >= 1 {
		return dim(fmt.Sprintf("%s ctx · %d%%", shortTokens(used), pct))
	}
	threshold := int(ratio * 100)
	left := max(threshold-pct, 0)
	body := fmt.Sprintf("%s ctx · %d%% left", shortTokens(used), left)
	color := activeCLITheme.muted
	if pct >= threshold {
		body = fmt.Sprintf("%s ctx · compact", shortTokens(used))
		color = activeCLITheme.danger
	} else if left <= 10 {
		color = activeCLITheme.warn
	}
	return themeFg(color, body)
}

func (m chatTUI) statusIdentitySingleRow(maxWidth int) string {
	if maxWidth < 8 || !m.presentation.StatusPath {
		return ""
	}
	workspace := m.workspaceLabel()
	hasGit := strings.TrimSpace(m.gitStatus.Repo) != "" && strings.TrimSpace(m.gitStatus.Branch) != ""
	switch {
	case workspace != "" && hasGit:
		git := m.gitStatus.RenderWithin(maxWidth, activeCLITheme.warn)
		if visibleWidth(workspace)+3+visibleWidth(git) <= maxWidth {
			return dim(workspace) + " · " + git
		}
		gitBudget := max(maxWidth/2, 8)
		git = m.gitStatus.RenderWithin(gitBudget, activeCLITheme.warn)
		workspaceBudget := maxWidth - visibleWidth(git) - 3
		if workspaceBudget >= 8 {
			return dim(compactMiddle(workspace, workspaceBudget)) + " · " + git
		}
		return dim(compactMiddle(workspace, maxWidth))
	case workspace != "":
		return dim(compactMiddle(workspace, maxWidth))
	case hasGit:
		return m.gitStatus.RenderWithin(maxWidth, activeCLITheme.warn)
	default:
		return ""
	}
}

func nonEmptyStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(ansi.Strip(value)) != "" {
			out = append(out, value)
		}
	}
	return out
}

// hideStatusHintWhenKeyNamesCannotFit keeps the readable Shift+Tab/Ctrl+Y
// spelling on normal terminals without hard-wrapping a single shortcut on an
// extremely narrow terminal. In that case the idle state remains visible and
// the optional shortcut help yields space to the composer.
func hideStatusHintWhenKeyNamesCannotFit(primary string, width int) string {
	hint := i18n.M.ChatStatusCycleHintCompact
	for group := range strings.SplitSeq(hint, " · ") {
		if visibleWidth(statusFooterIndent+group) > width {
			return strings.Replace(primary, " · "+footerHint(hint), "", 1)
		}
	}
	return primary
}

func statusFooterDivider(width int) string {
	width = max(width, 1)
	if width <= visibleWidth(statusFooterIndent) {
		return themeFg(activeCLITheme.border, strings.Repeat("─", width))
	}
	ruleWidth := width - visibleWidth(statusFooterIndent)
	return statusFooterIndent + themeFg(activeCLITheme.border, strings.Repeat("─", ruleWidth))
}

func layoutStatusSides(left, right string, width int) string {
	switch {
	case right == "":
		return wrapStatusGroups(left, width)
	case left == "":
		return rightAlignStatusGroup(right, width)
	}
	leftWidth := visibleWidth(left)
	rightWidth := visibleWidth(right)
	if leftWidth+statusFooterGroupGap+rightWidth <= width {
		return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
	}
	// Once the two semantic halves no longer fit, switch layout deliberately:
	// interaction groups wrap only at their separators, while model/work owns a
	// new left-aligned row. This avoids the floating right-side orphan seen when
	// a terminal crosses the medium-width breakpoint.
	return wrapStatusGroups(left, width) + "\n" + statusFooterIndent + right
}

func wrapStatusGroups(line string, width int) string {
	if width <= 0 || line == "" || visibleWidth(line) <= width {
		return line
	}
	groups := strings.Split(line, " · ")
	if len(groups) < 2 {
		return wrapStatusLine(line, width)
	}

	var rows []string
	current := groups[0]
	for _, group := range groups[1:] {
		candidate := current + " · " + group
		if visibleWidth(candidate) <= width {
			current = candidate
			continue
		}
		rows = append(rows, wrapStatusLine(current, width))
		current = statusFooterIndent + group
	}
	rows = append(rows, wrapStatusLine(current, width))
	return strings.Join(rows, "\n")
}

func rightAlignStatusGroup(group string, width int) string {
	if group == "" {
		return ""
	}
	if visibleWidth(group) <= width {
		return strings.Repeat(" ", width-visibleWidth(group)) + group
	}
	return wrapStatusLine(group, width)
}

func (m chatTUI) layoutGitTelemetry(width int) string {
	width = max(width, 1)
	telemetryGroups := m.statusTelemetryGroups()
	telemetry := strings.Join(telemetryGroups, "  ")
	available := max(width-visibleWidth(statusFooterIndent), 1)
	workspace := ""
	hasGit := false
	if m.presentation.StatusPath {
		workspace = m.workspaceLabel()
		hasGit = strings.TrimSpace(m.gitStatus.Repo) != "" && strings.TrimSpace(m.gitStatus.Branch) != ""
	}

	var identityLines []string
	switch {
	case workspace != "" && hasGit:
		git := m.gitStatus.RenderWithin(available, activeCLITheme.warn)
		if visibleWidth(workspace)+3+visibleWidth(git) <= available {
			identityLines = append(identityLines, statusFooterIndent+dim(workspace)+" · "+git)
		} else {
			// Give the Git identity enough room to retain branch and dirty state,
			// then spend the remaining columns on the workspace. If both cannot
			// remain legible on one row, keep them as two independently bounded
			// semantic rows instead of overflowing the terminal.
			gitBudget := max(available*2/3, 1)
			git = m.gitStatus.RenderWithin(gitBudget, activeCLITheme.warn)
			workspaceBudget := available - visibleWidth(git) - 3
			if workspaceBudget >= 8 {
				identityLines = append(identityLines, statusFooterIndent+dim(compactMiddle(workspace, workspaceBudget))+" · "+git)
			} else {
				identityLines = append(identityLines,
					statusFooterIndent+dim(compactMiddle(workspace, available)),
					statusFooterIndent+m.gitStatus.RenderWithin(available, activeCLITheme.warn),
				)
			}
		}
	case workspace != "":
		identityLines = append(identityLines, statusFooterIndent+dim(compactMiddle(workspace, available)))
	case hasGit:
		identityLines = append(identityLines, statusFooterIndent+m.gitStatus.RenderWithin(available, activeCLITheme.warn))
	}

	if len(identityLines) == 0 {
		return packStatusGroups(telemetryGroups, width)
	}
	if telemetry == "" {
		return strings.Join(identityLines, "\n")
	}

	telemetryWidth := visibleWidth(telemetry)
	last := identityLines[len(identityLines)-1]
	if visibleWidth(last)+statusFooterGroupGap+telemetryWidth <= width {
		identityLines[len(identityLines)-1] = last + strings.Repeat(" ", width-visibleWidth(last)-telemetryWidth) + telemetry
		return strings.Join(identityLines, "\n")
	}

	// Under width pressure Git gets its own full row instead of being shortened
	// merely to keep telemetry beside it. Telemetry then packs left-to-right by
	// semantic group, so no right-aligned fragment floats on a continuation row.
	return strings.Join(identityLines, "\n") + "\n" + packStatusGroups(telemetryGroups, width)
}

func packStatusGroups(groups []string, width int) string {
	width = max(width, 1)
	if len(groups) == 0 {
		return ""
	}
	indent := statusFooterIndent
	if width <= visibleWidth(indent) {
		indent = ""
	}

	var rows []string
	current := indent
	for _, group := range groups {
		if strings.TrimSpace(ansi.Strip(group)) == "" {
			continue
		}
		candidate := current + group
		if strings.TrimSpace(ansi.Strip(current)) != "" {
			candidate = current + "  " + group
		}
		if visibleWidth(candidate) <= width {
			current = candidate
			continue
		}
		if strings.TrimSpace(ansi.Strip(current)) != "" {
			rows = append(rows, current)
		}
		current = indent + group
		if visibleWidth(current) > width {
			rows = append(rows, wrapStatusLine(current, width))
			current = indent
		}
	}
	if strings.TrimSpace(ansi.Strip(current)) != "" {
		rows = append(rows, current)
	}
	return strings.Join(rows, "\n")
}
