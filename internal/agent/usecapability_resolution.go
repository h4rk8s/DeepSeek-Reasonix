package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/capability"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

const maxCapabilityRepairBytes = 4 << 10

type capabilityResolutionError struct {
	Code       string
	Action     string
	Reference  string
	Candidates []string
	Retry      string
	Message    string
}

func (e *capabilityResolutionError) Error() string {
	if e == nil {
		return "capability resolution failed"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "capability resolution failed (code=%s; remote_dispatched=false): %s", e.Code, e.Message)
	if len(e.Candidates) > 0 {
		fmt.Fprintf(&b, "; candidates=%s", strings.Join(e.Candidates, ","))
	}
	if e.Retry != "" {
		b.WriteString("; retry exactly once with ")
		b.WriteString(e.Retry)
	}
	return truncateCapabilityRepair(b.String())
}

func (t *UseCapabilityTool) resolveCapabilityReference(action, reference string, args useCapabilityArgs) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return t.resolveMissingCapabilityReference(action, args)
	}
	if canonical, ok := semanticCapabilityAlias(reference); ok {
		return canonical, nil
	}
	if canonical, ok := portableMCPReference(reference); ok {
		return canonical, nil
	}
	// Canonical IDs already carry their routing namespace. Keep them on the
	// original resolver path without consulting the live catalog: besides being
	// cheaper, this preserves the runtime's lock/snapshot ordering while MCP
	// enablement changes concurrently.
	if isCanonicalCapabilityID(reference) {
		return reference, nil
	}
	canonical, candidates := t.canonicalCapabilityReference(reference)
	switch len(candidates) {
	case 0:
		if canonical != "" {
			return canonical, nil
		}
		// Fully-qualified IDs can be absent from a cold catalog and still resolve
		// through the runtime (for example an unconnected MCP without a cache).
		if strings.Contains(reference, ":") {
			return reference, nil
		}
		return "", &capabilityResolutionError{
			Code: "unknown_capability_reference", Action: action, Reference: reference,
			Candidates: t.searchCandidateIDs(reference, 5),
			Message:    fmt.Sprintf("unknown capability reference %q", reference),
		}
	case 1:
		return candidates[0], nil
	default:
		return "", &capabilityResolutionError{
			Code: "ambiguous_capability_reference", Action: action, Reference: reference,
			Candidates: candidates,
			Message:    fmt.Sprintf("capability reference %q is ambiguous", reference),
		}
	}
}

func (t *UseCapabilityTool) resolveMissingCapabilityReference(action string, args useCapabilityArgs) (string, error) {
	query := strings.TrimSpace(args.Query)
	if query != "" {
		candidates := t.searchCapabilityResults(query, 5)
		if len(candidates) == 1 || uniqueExactSearchResult(query, candidates) {
			return candidates[0].CapabilityID, nil
		}
		code := "ambiguous_capability_query"
		if len(candidates) == 0 {
			code = "unknown_capability_query"
		}
		return "", &capabilityResolutionError{
			Code: code, Action: action, Reference: query,
			Candidates: capabilityResultIDs(candidates),
			Message:    fmt.Sprintf("query %q did not identify exactly one capability", query),
		}
	}

	candidates := t.argumentShapeCandidates(args.Arguments)
	retry := ""
	if len(candidates) == 1 {
		retry = marshalCapabilityRetry(action, candidates[0], args.Arguments)
	}
	return "", &capabilityResolutionError{
		Code: "missing_capability_id", Action: action, Candidates: candidates, Retry: retry,
		Message: "capability_id is required for action=" + action,
	}
}

func (t *UseCapabilityTool) canonicalCapabilityReference(reference string) (string, []string) {
	cat := t.currentCatalog()
	if entry, ok := cat.Lookup(reference); ok {
		return entry.ID, []string{entry.ID}
	}
	want := normalizeCapabilityAlias(reference)
	if want == "" {
		return "", nil
	}
	set := map[string]struct{}{}
	for _, entry := range cat.Entries {
		for _, alias := range capabilityEntryAliases(entry) {
			if normalizeCapabilityAlias(alias) == want {
				set[entry.ID] = struct{}{}
				break
			}
		}
	}
	if t.registry != nil {
		for _, binding := range t.registry.MCPBindings() {
			for _, alias := range append(tool.MCPBindingAliases(binding), "mcp/"+binding.Server+"/"+binding.RawName) {
				if normalizeCapabilityAlias(alias) == want {
					set[binding.CapabilityID] = struct{}{}
					break
				}
			}
		}
	}
	candidates := sortedCapabilityIDs(set)
	if len(candidates) == 1 {
		return candidates[0], candidates
	}
	return "", candidates
}

func semanticCapabilityAlias(reference string) (string, bool) {
	switch normalizeCapabilityAlias(reference) {
	case "memory:remember":
		return "tool:remember", true
	case "memory:forget":
		return "tool:forget", true
	case "session:list_sessions":
		return "tool:list_sessions", true
	case "session:read_session":
		return "tool:read_session", true
	case "session:set_session_title":
		return "tool:set_session_title", true
	default:
		return "", false
	}
}

func isCanonicalCapabilityID(reference string) bool {
	prefix, rest, ok := strings.Cut(reference, ":")
	if !ok || strings.TrimSpace(rest) == "" {
		return false
	}
	switch prefix {
	case "tool", "skill", "task", "workflow", "web", "lsp", "session", "memory":
		return true
	case "mcp-server":
		_, valid := parseMCPServerCapabilityID(reference)
		return valid
	case "mcp-tool":
		_, _, err := parseMCPCapabilityID(reference)
		return err == nil
	default:
		return false
	}
}

func capabilityEntryAliases(entry capability.Entry) []string {
	aliases := []string{entry.ID, entry.Name, entry.ToolName}
	if _, rest, ok := strings.Cut(entry.ID, ":"); ok {
		aliases = append(aliases, rest)
	}
	if entry.Kind == capability.KindMCPTool {
		server, raw, err := parseMCPCapabilityID(entry.ID)
		if err == nil {
			aliases = append(aliases,
				raw,
				server+"/"+raw,
				"mcp/"+server+"/"+raw,
				plugin.ModelToolName(server, raw),
			)
		}
	}
	switch strings.TrimSpace(entry.ToolName) {
	case "remember", "forget":
		aliases = append(aliases, "memory:"+strings.TrimSpace(entry.ToolName))
	case "list_sessions", "read_session", "set_session_title":
		aliases = append(aliases, "session:"+strings.TrimSpace(entry.ToolName))
	}
	return aliases
}

func portableMCPReference(reference string) (string, bool) {
	reference = strings.TrimSpace(reference)
	if !strings.HasPrefix(reference, "mcp/") {
		return "", false
	}
	parts := strings.Split(reference, "/")
	if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "", false
	}
	return "mcp-tool:" + strings.TrimSpace(parts[1]) + "/" + strings.TrimSpace(parts[2]), true
}

func normalizeCapabilityAlias(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sortedCapabilityIDs(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (t *UseCapabilityTool) searchCandidateIDs(query string, limit int) []string {
	return capabilityResultIDs(t.searchCapabilityResults(query, limit))
}

func capabilityResultIDs(results []capabilitySearchResult) []string {
	out := make([]string, 0, len(results))
	for _, result := range results {
		out = append(out, result.CapabilityID)
	}
	return out
}

func uniqueExactSearchResult(query string, results []capabilitySearchResult) bool {
	if len(results) == 0 || results[0].score < 8000 {
		return false
	}
	return len(results) == 1 || results[1].score < results[0].score
}

func (t *UseCapabilityTool) argumentShapeCandidates(raw json.RawMessage) []string {
	var supplied map[string]any
	if json.Unmarshal(raw, &supplied) != nil || len(supplied) == 0 {
		return nil
	}
	cat := t.currentCatalog()
	mcpSchemas := t.mcpSearchSchemaIndex()
	set := map[string]struct{}{}
	for _, entry := range cat.Entries {
		if entry.ID == "tool:use_capability" || entry.Status == capability.StatusDisabled || entry.Status == capability.StatusFailed {
			continue
		}
		schema := t.capabilitySchema(entry, mcpSchemas)
		properties, required := capabilitySchemaShape(schema)
		if len(properties) == 0 || !keysFitCapabilityShape(supplied, properties, required) {
			continue
		}
		set[entry.ID] = struct{}{}
	}
	return sortedCapabilityIDs(set)
}

func capabilitySchemaShape(raw json.RawMessage) (map[string]struct{}, []string) {
	var root struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return nil, nil
	}
	properties := make(map[string]struct{}, len(root.Properties))
	for name := range root.Properties {
		properties[name] = struct{}{}
	}
	return properties, root.Required
}

func keysFitCapabilityShape(supplied map[string]any, properties map[string]struct{}, required []string) bool {
	for name := range supplied {
		if _, ok := properties[name]; !ok {
			return false
		}
	}
	for _, name := range required {
		if _, ok := supplied[name]; !ok {
			return false
		}
	}
	return true
}

func marshalCapabilityRetry(action, id string, arguments json.RawMessage) string {
	if len(arguments) == 0 || string(arguments) == "null" {
		arguments = json.RawMessage(`{}`)
	}
	payload := struct {
		Action       string          `json:"action"`
		CapabilityID string          `json:"capability_id"`
		Arguments    json.RawMessage `json:"arguments"`
	}{Action: action, CapabilityID: id, Arguments: arguments}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return truncateCapabilityRepair(string(raw))
}

func truncateCapabilityRepair(value string) string {
	if len(value) <= maxCapabilityRepairBytes {
		return value
	}
	return value[:maxCapabilityRepairBytes-len("...[truncated]")] + "...[truncated]"
}
