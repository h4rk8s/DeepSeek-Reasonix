package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/session"
)

const resumeListCap = 10

// recentSessions returns the newest saved sessions under dir. It keeps recovery
// groups intact at the display cap (a single group may make the result slightly
// larger) so the 1-based indices match /resume <n> and its completion without
// orphaning a conflict copy from its parent. A read error yields an empty list.
func recentSessions(dir string) []agent.SessionInfo {
	if dir == "" {
		return nil
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil {
		return nil
	}
	sessions = orderResumeSessions(sessions)
	return capResumeSessionGroups(sessions, resumeListCap)
}

type resumeEntryKind uint8

const (
	resumeEntryLegacy resumeEntryKind = iota
	resumeEntryCanonical
	resumeEntryRetired
)

// resumeEntry is one picker row across all persistence generations. Legacy
// transcript data stays in session for the existing recovery-family behavior;
// canonical and retired directory stores stay typed and are never converted
// into fake transcript paths.
type resumeEntry struct {
	session agent.SessionInfo
	stored  session.SessionInfo
	kind    resumeEntryKind
	project string
}

func (e resumeEntry) isZero() bool {
	return e.session.Path == "" && e.stored.SessionID == "" && e.stored.Path == ""
}

const resumeOtherProjectsCap = 5

// resumeEntries preserves the legacy transcript picker contract. Compatibility
// controllers can emit canonical preview sidecars without owning the canonical
// lifecycle, so mixing those previews into their picker would silently retarget
// numeric selections away from the transcript they can actually resume.
func resumeEntries(dir string) []resumeEntry {
	base := recentSessions(dir)
	out := make([]resumeEntry, 0, len(base)+resumeOtherProjectsCap)
	for _, info := range base {
		out = append(out, resumeEntry{session: info, kind: resumeEntryLegacy})
	}
	out = append(out, otherProjectResumeEntries(dir)...)
	return out
}

// unifiedResumeEntries spans the final store, the immediately retired store,
// and legacy transcripts so an upgrade cannot hide the session the previous
// binary was just writing. Only exclusive canonical controllers may consume it.
func unifiedResumeEntries(dir string) []resumeEntry {
	base := localResumeEntries(dir, resumeListCap)
	out := make([]resumeEntry, 0, len(base)+resumeOtherProjectsCap)
	out = append(out, base...)
	out = append(out, unifiedOtherProjectResumeEntries(dir)...)
	return out
}

func resumeEntriesForController(dir string, ctrl control.SessionAPI) []resumeEntry {
	identity, ok := ctrl.(control.IdentityLifecycle)
	if ok && identity.UsesExclusiveSession() {
		return unifiedResumeEntries(dir)
	}
	return resumeEntries(dir)
}

func localResumeEntries(dir string, limit int) []resumeEntry {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	canonical := listCanonicalResumeEntries(dir)
	retired := listRetiredResumeEntries(dir)

	covered := make(map[string]bool, len(canonical)+len(retired))
	for _, entry := range canonical {
		if source := cleanResumeSource(entry.stored.SourcePath); source != "" {
			covered[source] = true
		}
	}
	for _, entry := range retired {
		path := cleanResumeSource(entry.stored.Path)
		if covered[path] {
			if source := cleanResumeSource(entry.stored.SourcePath); source != "" {
				covered[source] = true
			}
			continue
		}
		canonical = append(canonical, entry)
		if source := cleanResumeSource(entry.stored.SourcePath); source != "" {
			covered[source] = true
		}
	}

	legacy, err := agent.ListSessions(dir)
	if err == nil {
		for _, info := range orderResumeSessions(legacy) {
			if !covered[cleanResumeSource(info.Path)] {
				canonical = append(canonical, resumeEntry{session: info, kind: resumeEntryLegacy})
			}
		}
	}
	return orderAndCapResumeEntries(canonical, limit)
}

func cleanResumeSource(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if canonical, err := filepath.Abs(path); err == nil {
		return filepath.Clean(canonical)
	}
	return filepath.Clean(path)
}

type sessionPageLister interface {
	List(context.Context, string, int) (session.SessionPage, error)
}

func listStoredResumeSessions(lister sessionPageLister, hostID string) []session.SessionInfo {
	if lister == nil {
		return nil
	}
	ctx := context.Background()
	cursor := ""
	var out []session.SessionInfo
	for {
		page, err := lister.List(ctx, cursor, 100)
		if err != nil {
			return out
		}
		for _, info := range page.Sessions {
			if info.Error != "" {
				continue
			}
			if info.Ref.SessionID == "" {
				info.Ref = session.SessionRef{HostID: hostID, SessionID: info.SessionID}
			}
			out = append(out, info)
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	return out
}

func listCanonicalResumeEntries(dir string) []resumeEntry {
	service := cliSessionService(dir)
	if service == nil || service.Query() == nil {
		return nil
	}
	infos := listStoredResumeSessions(service.Query(), service.HostID())
	out := make([]resumeEntry, 0, len(infos))
	for _, info := range infos {
		out = append(out, resumeEntry{stored: info, kind: resumeEntryCanonical})
	}
	return out
}

func listRetiredResumeEntries(dir string) []resumeEntry {
	root := session.RetiredRootForLegacyDir(dir)
	if root == "" {
		return nil
	}
	infos := listStoredResumeSessions(session.NewFilesystemPersistence(root), "local")
	out := make([]resumeEntry, 0, len(infos))
	for _, info := range infos {
		out = append(out, resumeEntry{stored: info, kind: resumeEntryRetired})
	}
	return out
}

func (e resumeEntry) path() string {
	if e.kind == resumeEntryLegacy {
		return e.session.Path
	}
	return e.stored.Path
}

func (e resumeEntry) updatedAt() time.Time {
	if e.kind == resumeEntryLegacy {
		return e.session.ModTime
	}
	return e.stored.UpdatedAt
}

func (e resumeEntry) key() string {
	switch e.kind {
	case resumeEntryCanonical:
		return "canonical:" + e.stored.Ref.HostID + ":" + e.stored.Ref.SessionID
	case resumeEntryRetired:
		return "retired:" + e.stored.Path
	default:
		return "legacy:" + e.session.Path
	}
}

func (e resumeEntry) isActive(ctrl control.SessionAPI) bool {
	if ctrl == nil {
		return false
	}
	if e.kind == resumeEntryCanonical {
		identity, ok := ctrl.(control.IdentityLifecycle)
		if !ok {
			return false
		}
		ref, ok := identity.SessionRef()
		return ok && ref == e.stored.Ref
	}
	return e.kind == resumeEntryLegacy && e.session.Path == ctrl.SessionPath()
}

func (e resumeEntry) turns() int {
	if e.kind == resumeEntryLegacy {
		return e.session.Turns
	}
	return e.stored.Turns
}

func (e resumeEntry) displayTitle() string {
	if e.kind == resumeEntryLegacy {
		if e.session.CustomTitle != "" {
			return e.session.CustomTitle
		}
		if e.session.TopicTitle != "" {
			return e.session.TopicTitle
		}
		return e.session.Preview
	}
	if e.stored.Title != "" {
		return e.stored.Title
	}
	return e.stored.Preview
}

func (e resumeEntry) summary() string {
	preview := e.displayTitle()
	if preview == "" {
		preview = "(no user message yet)"
	}
	prefix := ""
	if e.kind == resumeEntryLegacy {
		prefix = recoverySessionBadge(e.session)
	}
	return prefix + fmt.Sprintf("%d turns · %s", e.turns(), preview)
}

func (e resumeEntry) modelSelection() (string, string) {
	if e.kind == resumeEntryLegacy {
		model, identity, _ := agent.LoadSessionModelSelection(e.session.Path)
		return model, identity
	}
	return e.stored.ModelRef, e.stored.ModelIdentity
}

func hydrateResumeEntry(ctx context.Context, entry resumeEntry) (resumeEntry, error) {
	if entry.kind == resumeEntryLegacy {
		return entry, nil
	}
	root := filepath.Dir(entry.stored.Path)
	var query *session.Query
	if entry.kind == resumeEntryCanonical {
		service := cliSessionServiceForRoot(root)
		if service == nil {
			return resumeEntry{}, fmt.Errorf("resume: canonical session service is unavailable")
		}
		query = service.Query()
	} else {
		service, err := session.NewService(entry.stored.Ref.HostID, session.NewFilesystemPersistence(root))
		if err != nil {
			return resumeEntry{}, err
		}
		query = service.Query()
		defer query.Close()
	}
	info, err := query.Get(ctx, entry.stored.Ref)
	if err != nil {
		return resumeEntry{}, err
	}
	entry.stored = info
	return entry, nil
}

func resolveResumeEntry(dir, query string) (resumeEntry, error) {
	query = strings.TrimSpace(query)
	if query == "" || query == resumePickerSentinel {
		return resumeEntry{}, nil
	}
	if fileInfo, err := os.Stat(query); err == nil {
		absolute, absErr := filepath.Abs(query)
		if absErr != nil {
			return resumeEntry{}, absErr
		}
		if fileInfo.Mode().IsRegular() {
			if _, loadErr := loadResumableSession(absolute); loadErr != nil {
				return resumeEntry{}, loadErr
			}
			return resumeEntry{session: agent.SessionInfo{Path: absolute, ModTime: fileInfo.ModTime()}, kind: resumeEntryLegacy}, nil
		}
		if fileInfo.IsDir() {
			root, id := filepath.Dir(absolute), filepath.Base(absolute)
			info, statErr := session.NewFilesystemPersistence(root).Stat(context.Background(), id)
			if statErr != nil {
				return resumeEntry{}, statErr
			}
			info.Ref = session.SessionRef{HostID: "local", SessionID: info.SessionID}
			kind := resumeEntryCanonical
			if filepath.Base(root) == "sessions-v3" {
				kind = resumeEntryRetired
			}
			return hydrateResumeEntry(context.Background(), resumeEntry{stored: info, kind: kind})
		}
	}
	entries := localResumeEntries(dir, 0)
	if looksLikeMachineSessionID(query) {
		key, err := loadMachineIdentityKey()
		if err != nil {
			return resumeEntry{}, fmt.Errorf("machine identity is unavailable: %w", err)
		}
		for _, entry := range entries {
			id := entry.stored.SessionID
			if entry.kind == resumeEntryLegacy {
				id = agent.BranchID(entry.session.Path)
			}
			if machineSessionIDWithKey(id, key) == query {
				return entry, nil
			}
		}
		return resumeEntry{}, fmt.Errorf("no session matches %q", query)
	}
	lower := strings.ToLower(query)
	var exact []resumeEntry
	var partial []resumeEntry
	for _, entry := range entries {
		exactMatch, partialMatch := resumeEntryMatchesQuery(entry, query, lower)
		if exactMatch {
			exact = append(exact, entry)
			continue
		}
		if partialMatch {
			partial = append(partial, entry)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = partial
	}
	switch len(matches) {
	case 0:
		return resumeEntry{}, fmt.Errorf("no session matches %q", query)
	case 1:
		return hydrateResumeEntry(context.Background(), matches[0])
	default:
		return resumeEntry{}, fmt.Errorf("session query %q is ambiguous (%d matches)", query, len(matches))
	}
}

func orderAndCapResumeEntries(entries []resumeEntry, limit int) []resumeEntry {
	if len(entries) == 0 {
		return nil
	}
	type group struct {
		entries  []resumeEntry
		activity time.Time
		order    int
	}
	legacyByID := map[string]agent.SessionInfo{}
	for _, entry := range entries {
		if entry.kind == resumeEntryLegacy {
			legacyByID[agent.BranchID(entry.session.Path)] = entry.session
		}
	}
	groups := map[string]*group{}
	ordered := make([]*group, 0, len(entries))
	for i, entry := range entries {
		key := entry.key()
		if entry.kind == resumeEntryLegacy {
			key = "legacy-group:" + recoveryResumeGroupKey(entry.session, legacyByID)
		}
		g := groups[key]
		if g == nil {
			g = &group{order: i}
			groups[key] = g
			ordered = append(ordered, g)
		}
		g.entries = append(g.entries, entry)
		if entry.updatedAt().After(g.activity) {
			g.activity = entry.updatedAt()
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].activity.Equal(ordered[j].activity) {
			return ordered[i].order < ordered[j].order
		}
		return ordered[i].activity.After(ordered[j].activity)
	})
	out := make([]resumeEntry, 0, len(entries))
	for _, g := range ordered {
		if limit > 0 && len(out) > 0 && len(out)+len(g.entries) > limit {
			break
		}
		out = append(out, g.entries...)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func otherProjectResumeEntries(excludeDir string) []resumeEntry {
	type target struct {
		path string
		root string
	}
	var targets []target
	for _, t := range defaultSessionCatalogTargets() {
		if t.Scope != "project" || t.Path == "" {
			continue
		}
		targets = append(targets, target{path: t.Path, root: t.WorkspaceRoot})
	}
	exclude := filepath.Clean(excludeDir)
	var out []resumeEntry
	for _, t := range targets {
		if filepath.Clean(t.path) == exclude {
			continue
		}
		sessions, err := agent.ListSessions(t.path)
		if err != nil || len(sessions) == 0 {
			continue
		}
		name := filepath.Base(strings.TrimRight(t.root, string(filepath.Separator)))
		if name == "" || name == "." {
			name = t.root
		}
		out = append(out, resumeEntry{
			session: sessions[0],
			kind:    resumeEntryLegacy,
			project: name,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].updatedAt().After(out[j].updatedAt())
	})
	if len(out) > resumeOtherProjectsCap {
		out = out[:resumeOtherProjectsCap]
	}
	return out
}

func unifiedOtherProjectResumeEntries(excludeDir string) []resumeEntry {
	type target struct {
		path string
		root string
	}
	var targets []target
	for _, t := range defaultSessionCatalogTargets() {
		if t.Scope != "project" || t.Path == "" {
			continue
		}
		targets = append(targets, target{path: t.Path, root: t.WorkspaceRoot})
	}
	exclude := filepath.Clean(excludeDir)
	var out []resumeEntry
	for _, t := range targets {
		if filepath.Clean(t.path) == exclude {
			continue
		}
		entries := localResumeEntries(t.path, 1)
		if len(entries) == 0 {
			continue
		}
		name := filepath.Base(strings.TrimRight(t.root, string(filepath.Separator)))
		if name == "" || name == "." {
			name = t.root
		}
		entry := entries[0]
		entry.project = name
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].updatedAt().After(out[j].updatedAt())
	})
	if len(out) > resumeOtherProjectsCap {
		out = out[:resumeOtherProjectsCap]
	}
	return out
}

// mostRecentSession returns the chronologically newest saved session for
// --continue. Interactive resume surfaces deliberately group recovery families
// and prefer visible leaves, but --continue promises the most recent session and
// must not let that presentation ordering select an older recovery copy.
func mostRecentResumeEntry(dir string) (resumeEntry, bool) {
	entries := localResumeEntries(dir, 0)
	if len(entries) == 0 {
		return resumeEntry{}, false
	}
	newest := entries[0]
	for _, entry := range entries[1:] {
		if entry.updatedAt().After(newest.updatedAt()) {
			newest = entry
		}
	}
	return newest, true
}

func mostRecentSession(dir string) (agent.SessionInfo, bool) {
	if dir == "" {
		return agent.SessionInfo{}, false
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil || len(sessions) == 0 {
		return agent.SessionInfo{}, false
	}
	return sessions[0], true
}

func capResumeSessionGroups(sessions []agent.SessionInfo, limit int) []agent.SessionInfo {
	if limit <= 0 || len(sessions) <= limit {
		return sessions
	}
	byID := make(map[string]agent.SessionInfo, len(sessions))
	for _, session := range sessions {
		byID[agent.BranchID(session.Path)] = session
	}
	out := make([]agent.SessionInfo, 0, limit)
	for start := 0; start < len(sessions); {
		key := recoveryResumeGroupKey(sessions[start], byID)
		end := start + 1
		for end < len(sessions) && recoveryResumeGroupKey(sessions[end], byID) == key {
			end++
		}
		if len(out) > 0 && len(out)+(end-start) > limit {
			break
		}
		out = append(out, sessions[start:end]...)
		start = end
		if len(out) >= limit {
			break
		}
	}
	return out
}

// orderResumeSessions keeps conflict-recovery copies next to the session they
// came from. Groups remain newest-first, while the newest visible leaf is first
// within each group so interactive picker and numbered resume surfaces present
// the most likely writable continuation before its ancestors.
func orderResumeSessions(sessions []agent.SessionInfo) []agent.SessionInfo {
	if len(sessions) < 2 {
		return sessions
	}
	byID := make(map[string]agent.SessionInfo, len(sessions))
	for _, session := range sessions {
		byID[agent.BranchID(session.Path)] = session
	}
	type resumeGroup struct {
		items    []agent.SessionInfo
		newest   int
		activity int64
	}
	groups := make(map[string]*resumeGroup, len(sessions))
	order := make([]*resumeGroup, 0, len(sessions))
	for i, session := range sessions {
		key := recoveryResumeGroupKey(session, byID)
		group := groups[key]
		if group == nil {
			group = &resumeGroup{newest: i}
			groups[key] = group
			order = append(order, group)
		}
		group.items = append(group.items, session)
		if stamp := session.ModTime.UnixNano(); stamp > group.activity {
			group.activity = stamp
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].activity == order[j].activity {
			return order[i].newest < order[j].newest
		}
		return order[i].activity > order[j].activity
	})

	out := make([]agent.SessionInfo, 0, len(sessions))
	for _, group := range order {
		children := make(map[string]bool, len(group.items))
		members := make(map[string]bool, len(group.items))
		for _, session := range group.items {
			members[agent.BranchID(session.Path)] = true
		}
		for _, session := range group.items {
			parentID := strings.TrimSpace(session.ParentID)
			if members[parentID] {
				children[parentID] = true
			}
		}
		sort.SliceStable(group.items, func(i, j int) bool {
			iLeaf := !children[agent.BranchID(group.items[i].Path)]
			jLeaf := !children[agent.BranchID(group.items[j].Path)]
			if iLeaf != jLeaf {
				return iLeaf
			}
			return group.items[i].ModTime.After(group.items[j].ModTime)
		})
		out = append(out, group.items...)
	}
	return out
}

func recoveryResumeGroupKey(session agent.SessionInfo, byID map[string]agent.SessionInfo) string {
	id := agent.BranchID(session.Path)
	if !session.Recovered {
		return id
	}
	seen := map[string]bool{id: true}
	current := session
	for {
		parentID := strings.TrimSpace(current.ParentID)
		if parentID == "" {
			return agent.BranchID(current.Path)
		}
		if seen[parentID] {
			return "recovery-cycle:" + parentID
		}
		seen[parentID] = true
		parent, ok := byID[parentID]
		if !ok {
			return "recovery-parent:" + parentID
		}
		if !parent.Recovered {
			return parentID
		}
		current = parent
	}
}

// runResumeCommand handles "/resume": with no argument it opens the recent
// session picker; "/resume <n>" loads that
// session into the running controller in place — keeping the current model and
// replaying the transcript into scrollback.
func (m *chatTUI) runResumeCommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/resume"
	if len(args) < 2 {
		m.openResumePicker()
		return
	}
	// Do not run recovery GC between displaying/completing a numeric index and
	// resolving it here. Removing an earlier row would silently retarget the
	// user's already-selected number. Bare /resume performs cleanup before it
	// builds the picker, and startup performs the ordinary background sweep.
	entries := resumeEntriesForController(m.ctrl.SessionDir(), m.ctrl)
	if len(entries) == 0 {
		m.notice(i18n.M.NoSessionToResume)
		return
	}
	if m.ctrl.Running() {
		m.notice(i18n.M.ResumeBusy)
		return
	}
	idx, err := strconv.Atoi(strings.TrimSpace(args[1]))
	if err != nil || idx < 1 || idx > len(entries) {
		m.notice(fmt.Sprintf(i18n.M.ResumeBadIndexFmt, len(entries)))
		return
	}
	target := entries[idx-1]
	if target.isActive(m.ctrl) {
		m.notice(i18n.M.ResumeAlreadyActive)
		return
	}
	// Persist the conversation we're leaving so switching back later restores it.
	// Snapshot before moving the lease: the outgoing session must be written
	// while this process still owns it.
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("resume: snapshot current session: " + err.Error())
		return
	}
	m.followSessionLease()
	if err := m.commitResumeEntry(target); err != nil {
		message := err.Error()
		if target.kind == resumeEntryLegacy {
			message = sessionLeaseHeldNotice(err)
		}
		m.notice("resume: " + message)
		if target.kind == resumeEntryLegacy && cliSessionTakeoverCandidate(err) {
			m.pendingTakeoverPath = target.session.Path
			m.notice("run /takeover to take this session over from the resident serve")
		}
		return
	}
	m.replayActiveBranch(i18n.M.ResumedTitle)
}

// runTakeoverCommand handles "/takeover": it force-takes the last refused
// resume target (or an explicit index/path argument) from the resident serve
// on this machine, then resumes it.
func (m *chatTUI) runTakeoverCommand(input string) {
	m.echoLocalCommand(input)
	args := tokenizeArgs(input) // args[0] == "/takeover"
	target := strings.TrimSpace(m.pendingTakeoverPath)
	if len(args) >= 2 {
		target = strings.TrimSpace(args[1])
		if idx, err := strconv.Atoi(target); err == nil {
			entries := resumeEntriesForController(m.ctrl.SessionDir(), m.ctrl)
			if idx < 1 || idx > len(entries) {
				m.notice(fmt.Sprintf(i18n.M.ResumeBadIndexFmt, len(entries)))
				return
			}
			entry := entries[idx-1]
			if entry.kind != resumeEntryLegacy {
				m.notice("takeover: canonical sessions use their storage writer lock and cannot be taken over through the legacy mirror")
				return
			}
			target = entry.session.Path
		}
	}
	if target == "" {
		m.notice("takeover: no refused session; run /resume <n> first or pass an index")
		return
	}
	if m.ctrl.Running() {
		m.notice(i18n.M.ResumeBusy)
		return
	}
	_, err := loadResumableSession(target)
	if err != nil {
		m.notice("takeover: " + err.Error())
		return
	}
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("takeover: snapshot current session: " + err.Error())
		return
	}
	m.followSessionLease()
	binding, bindErr := cliAcquireFreeSession(target, m.leases, m.takeover)
	if bindErr != nil {
		if !cliSessionTakeoverCandidate(bindErr) {
			m.notice("takeover: " + sessionLeaseHeldNotice(bindErr))
			return
		}
		m.notice("taking the session over from the resident serve…")
		binding, err = cliTakeoverHeldSession(target, bindErr, m.leases, m.takeover)
		if err != nil {
			m.notice("takeover: " + err.Error())
			return
		}
	}
	loaded, err := cliPrepareTakeoverCandidate(binding, m.leases)
	if err != nil {
		_ = cliReturnFailedTakeover(binding, m.leases, m.takeover)
		m.notice("takeover: " + err.Error())
		return
	}
	if err := binding.commitPrevious(m.takeover); err != nil {
		_ = cliReturnFailedTakeover(binding, m.leases, m.takeover)
		m.notice("takeover: " + err.Error())
		return
	}
	m.ctrl.Resume(loaded, target)
	if err := bindChatTUIAuthority(m); err != nil {
		m.notice("takeover: " + err.Error())
		return
	}
	m.pendingTakeoverPath = ""
	if m.takeover != nil && binding.grant.MirrorID != "" {
		m.takeover.AttachController(m.ctrl)
		m.takeover.Activate(binding)
	}
	m.replayActiveBranch(i18n.M.ResumedTitle)
	m.notice("session taken over; the remote side is now read-only and can take it back")
}

// resumeArgItems completes the index argument of "/resume <n>": once past the
// command word it lists recent sessions, inserting the 1-based index and
// showing timestamp + turn count + preview as the hint. Indices match
// the picker because both window through recentSessions.
func (m *chatTUI) resumeArgItems(val string) ([]compItem, int, bool) {
	cmdEnd := strings.IndexAny(val, " \t")
	if cmdEnd < 0 || val[:cmdEnd] != "/resume" {
		return nil, 0, false
	}
	from := strings.LastIndexAny(val, " \t") + 1
	if len(strings.Fields(val[:from])) != 1 || m.ctrl == nil {
		return nil, from, true
	}
	cur := val[from:]
	var out []compItem
	for i, entry := range resumeEntriesForController(m.ctrl.SessionDir(), m.ctrl) {
		idx := strconv.Itoa(i + 1)
		if cur != "" && !strings.HasPrefix(idx, cur) {
			continue
		}
		hint := fmt.Sprintf("%s · %s", entry.updatedAt().Local().Format("01-02 15:04"), entry.summary())
		if entry.project != "" {
			hint = fmt.Sprintf("[%s] %s", entry.project, hint)
		}
		out = append(out, compItem{label: idx, insert: idx, hint: hint})
	}
	return out, from, true
}

func recoverySessionBadge(s agent.SessionInfo) string {
	if !s.Recovered {
		return ""
	}
	parent := strings.TrimSpace(s.ParentID)
	if len(parent) > 8 {
		parent = parent[:8]
	}
	if parent == "" {
		parent = "?"
	}
	return fmt.Sprintf(i18n.M.ResumeRecoveryBadgeFmt, parent) + " "
}
