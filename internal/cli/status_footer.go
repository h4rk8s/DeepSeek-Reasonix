package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

const (
	statusFooterIndent   = "  "
	statusFooterGroupGap = 2
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
	fields := make([]string, 0, 3)
	for _, field := range []string{m.modelComboTag(), m.effortTag()} {
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
		if body, rate, ok := m.cacheStatus(); ok {
			data = append(data, themeFg(cacheStatusColor(rate), body))
		}
		if context := m.contextTag(); context != "" {
			data = append(data, context)
		}
		if jt := m.jobsTag(); jt != "" {
			data = append(data, jt)
		}
	}
	if balance := m.balanceTag(); balance != "" {
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
	modelWork := m.statusModelWorkGroup(max(width-visibleWidth(statusFooterIndent), 1))
	first := layoutStatusSides(primary, modelWork, width)
	second := m.layoutGitTelemetry(width)
	if second == "" {
		return first
	}
	return first + "\n" + statusFooterDivider(width) + "\n" + second
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
	workspace := m.workspaceLabel()
	hasGit := strings.TrimSpace(m.gitStatus.Repo) != "" && strings.TrimSpace(m.gitStatus.Branch) != ""

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
		if visibleWidth(telemetry) > width {
			return packStatusGroups(telemetryGroups, width)
		}
		return rightAlignStatusGroup(telemetry, width)
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
