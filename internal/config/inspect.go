package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	ConfigSourceBuiltin = "builtin"
	ConfigSourceUser    = "user"
	ConfigSourceProject = "project"

	ConfigSourceMissing = "missing"
	ConfigSourceValid   = "valid"
	ConfigSourceInvalid = "invalid"
)

// ConfigInspection is a redacted, read-only projection of the effective
// configuration and the declarations that produced it.
type ConfigInspection struct {
	SchemaVersion      int                      `json:"schema_version"`
	Root               string                   `json:"root"`
	Valid              bool                     `json:"valid"`
	EffectiveAvailable bool                     `json:"effective_available"`
	Sources            []ConfigSourceInspection `json:"sources"`
	Settings           []ConfigSetting          `json:"settings,omitempty"`
	Diagnostics        []ConfigDiagnostic       `json:"diagnostics,omitempty"`
	Warnings           []string                 `json:"warnings,omitempty"`
	Errors             []string                 `json:"errors,omitempty"`
	Effective          *Config                  `json:"-"`
}

type ConfigSourceInspection struct {
	Scope  string `json:"scope"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Loaded bool   `json:"loaded"`
	Error  string `json:"error,omitempty"`
}

type ConfigSetting struct {
	Path       string `json:"path"`
	Value      any    `json:"value"`
	Source     string `json:"source"`
	SourcePath string `json:"source_path,omitempty"`
	Note       string `json:"note,omitempty"`
}

type ConfigDiagnostic struct {
	Level      string `json:"level"`
	Code       string `json:"code"`
	Scope      string `json:"scope"`
	Path       string `json:"path"`
	SourcePath string `json:"source_path,omitempty"`
	Message    string `json:"message"`
}

type inspectedConfigSource struct {
	report ConfigSourceInspection
	meta   toml.MetaData
}

// InspectForRoot explains config precedence without resolving credentials or
// modifying any source file. Invalid user config leaves effective values
// unavailable because ordinary runtime startup fails in the same situation.
func InspectForRoot(root string) ConfigInspection {
	root = inspectionAbsoluteRoot(root)
	user := inspectConfigSource(ConfigSourceUser, userConfigLoadPath())
	project := inspectConfigSource(ConfigSourceProject, filepath.Join(root, "reasonix.toml"))
	report := ConfigInspection{
		SchemaVersion: 1,
		Root:          root,
		Valid:         sourceValid(user) && sourceValid(project),
		Sources: []ConfigSourceInspection{
			{Scope: ConfigSourceBuiltin, Status: ConfigSourceValid},
			user.report,
			project.report,
		},
	}
	report.Diagnostics = append(report.Diagnostics, sourceDiagnostics(user, false)...)
	report.Diagnostics = append(report.Diagnostics, sourceDiagnostics(project, true)...)
	for _, source := range []inspectedConfigSource{user, project} {
		if source.report.Status == ConfigSourceInvalid {
			report.Errors = append(report.Errors, source.report.Error)
		}
	}
	if user.report.Status == ConfigSourceInvalid {
		sortConfigDiagnostics(report.Diagnostics)
		return report
	}

	cfg, err := LoadForRootWithoutCredentialsReadOnly(root)
	if err != nil {
		report.Valid = false
		report.Errors = appendUniqueString(report.Errors, err.Error())
		sortConfigDiagnostics(report.Diagnostics)
		return report
	}
	report.EffectiveAvailable = true
	report.Effective = cfg
	report.Warnings = cfg.LoadWarnings()
	report.Sources[0].Loaded = true
	if user.report.Status == ConfigSourceValid {
		report.Sources[1].Loaded = true
	}
	if project.report.Status == ConfigSourceValid {
		report.Sources[2].Loaded = true
	}
	report.Settings = effectiveConfigSettings(cfg, user, project)
	report.Diagnostics = append(report.Diagnostics, runtimeConfigDiagnostics(cfg, project)...)
	sortConfigDiagnostics(report.Diagnostics)
	return report
}

func inspectionAbsoluteRoot(root string) string {
	root = resolveRoot(root)
	if abs, err := filepath.Abs(root); err == nil {
		return filepath.Clean(abs)
	}
	return root
}

func inspectConfigSource(scope, path string) inspectedConfigSource {
	source := inspectedConfigSource{report: ConfigSourceInspection{Scope: scope, Path: path, Status: ConfigSourceMissing}}
	if strings.TrimSpace(path) == "" {
		return source
	}
	_, exists, err := statConfigPath(path)
	if err != nil {
		source.report.Status = ConfigSourceInvalid
		source.report.Error = err.Error()
		return source
	}
	if !exists {
		return source
	}
	var decoded Config
	meta, err := decodeTOMLFile(path, &decoded)
	if err != nil {
		source.report.Status = ConfigSourceInvalid
		source.report.Error = fmt.Sprintf("config %s: %v", path, err)
		return source
	}
	source.report.Status = ConfigSourceValid
	source.meta = meta
	return source
}

func sourceValid(source inspectedConfigSource) bool {
	return source.report.Status != ConfigSourceInvalid
}

type ignoredConfigRule struct {
	path    []string
	code    string
	message string
}

var projectIgnoredConfigRules = []ignoredConfigRule{
	{[]string{"credentials_store"}, "project_scope_ignored", "credential storage is user-global"},
	{[]string{"cli"}, "project_scope_ignored", "CLI update behavior is user-global"},
	{[]string{"secrets"}, "project_scope_ignored", "secret protection is user-global"},
	{[]string{"remote"}, "project_scope_ignored", "remote hosts and routes are user-global"},
	{[]string{"telemetry"}, "project_scope_ignored", "telemetry is a user-global privacy choice"},
	{[]string{"desktop", "language"}, "project_scope_ignored", "desktop language is user-global"},
	{[]string{"desktop", "currency"}, "project_scope_ignored", "desktop pricing currency is user-global"},
	{[]string{"billing", "display_currency"}, "project_scope_ignored", "billing display currency is user-global"},
	{[]string{"agent", "memory_recall"}, "project_scope_ignored", "memory recall policy is user-global"},
	{[]string{"agent", "legacy_anchor_safety_gate"}, "project_scope_ignored", "legacy anchor safety is user-global"},
}

var retiredConfigRules = []ignoredConfigRule{
	{[]string{"agent", "max_steps"}, "retired_key", "interactive agent step limits are retired"},
	{[]string{"agent", "planner_max_steps"}, "retired_key", "planner step limits are retired"},
	{[]string{"agent", "auto_plan"}, "retired_key", "automatic plan mode is retired"},
	{[]string{"agent", "auto_plan_classifier"}, "retired_key", "automatic plan classification is retired"},
	{[]string{"agent", "soft_compact_ratio"}, "retired_key", "multi-threshold compaction is retired"},
	{[]string{"agent", "tool_result_snip_ratio"}, "retired_key", "multi-threshold compaction is retired"},
	{[]string{"agent", "compact_force_ratio"}, "retired_key", "multi-threshold compaction is retired"},
	{[]string{"agent", "cold_resume_prune"}, "retired_key", "multi-threshold compaction is retired"},
	{[]string{"agent", "context_editing"}, "retired_key", "native context editing is retired"},
	{[]string{"agent", "memory_compiler"}, "retired_key", "the Memory v5 execution compiler is retired"},
	{[]string{"secrets", "redact_tool_output"}, "retired_key", "tool-output redaction is retired"},
}

func sourceDiagnostics(source inspectedConfigSource, project bool) []ConfigDiagnostic {
	if source.report.Status != ConfigSourceValid {
		return nil
	}
	var diagnostics []ConfigDiagnostic
	if project {
		diagnostics = append(diagnostics, diagnosticsForRules(source, projectIgnoredConfigRules)...)
	}
	diagnostics = append(diagnostics, diagnosticsForRules(source, retiredConfigRules)...)
	for _, key := range source.meta.Undecoded() {
		path := key.String()
		if coveredByRule(path, retiredConfigRules) {
			continue
		}
		diagnostics = append(diagnostics, ConfigDiagnostic{
			Level:      "warning",
			Code:       "unknown_key",
			Scope:      source.report.Scope,
			Path:       path,
			SourcePath: source.report.Path,
			Message:    "unknown key is preserved on disk but has no runtime effect",
		})
	}
	return diagnostics
}

func diagnosticsForRules(source inspectedConfigSource, rules []ignoredConfigRule) []ConfigDiagnostic {
	var diagnostics []ConfigDiagnostic
	for _, rule := range rules {
		if !source.meta.IsDefined(rule.path...) {
			continue
		}
		diagnostics = append(diagnostics, ConfigDiagnostic{
			Level:      "warning",
			Code:       rule.code,
			Scope:      source.report.Scope,
			Path:       strings.Join(rule.path, "."),
			SourcePath: source.report.Path,
			Message:    rule.message,
		})
	}
	return diagnostics
}

func coveredByRule(path string, rules []ignoredConfigRule) bool {
	for _, rule := range rules {
		prefix := strings.Join(rule.path, ".")
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}

type configSettingSpec struct {
	path       string
	value      any
	candidates [][]string
}

func effectiveConfigSettings(cfg *Config, user, project inspectedConfigSource) []ConfigSetting {
	permissionMode := strings.TrimSpace(cfg.Permissions.Mode)
	if permissionMode == "" {
		permissionMode = "ask"
	}
	searchEngine := strings.TrimSpace(cfg.Tools.Search.Engine)
	if searchEngine == "" {
		searchEngine = "auto"
	}
	shellPrefer := strings.TrimSpace(cfg.Tools.Shell.Prefer)
	if shellPrefer == "" {
		shellPrefer = "auto"
	}
	imageCommandConfigured := strings.TrimSpace(cfg.Agent.ImageUnderstandingCommand) != ""
	specs := []configSettingSpec{
		settingSpec("default_model", cfg.DefaultModel),
		settingSpec("agent.planner_model", cfg.Agent.PlannerModel),
		settingSpec("agent.vision_model", cfg.Agent.VisionModel),
		settingSpec("agent.image_understanding_model", cfg.Agent.ImageUnderstandingModel),
		settingSpec("agent.image_understanding_command", imageCommandConfigured),
		settingSpec("agent.reasoning_language", cfg.ReasoningLanguage()),
		settingSpec("agent.compact_ratio", cfg.Agent.CompactRatio),
		settingSpec("ui.theme", cfg.UITheme()),
		settingSpec("ui.theme_style", cfg.UIThemeStyle()),
		settingSpec("ui.cursor_shape", cfg.UICursorShape()),
		settingSpec("ui.lazy_reasoning", cfg.UI.LazyReasoning),
		settingSpec("ui.image_understanding_log", cfg.UIImageUnderstandingLog()),
		settingSpec("ui.show_usage", cfg.UIShowUsage()),
		settingSpec("permissions.mode", permissionMode),
		settingSpec("sandbox.bash", cfg.BashMode()),
		settingSpec("sandbox.network", cfg.Sandbox.Network),
		settingSpec("tools.search.engine", searchEngine),
		settingSpec("tools.shell.prefer", shellPrefer),
	}
	settings := make([]ConfigSetting, 0, len(specs))
	for _, spec := range specs {
		scope, path := configSettingSource(spec.candidates, user, project)
		setting := ConfigSetting{Path: spec.path, Value: spec.value, Source: scope, SourcePath: path}
		if spec.path == "default_model" && cfg.IgnoredProjectDefaultModel() != "" {
			scope, path = configSettingSource([][]string{{"default_model"}}, user, inspectedConfigSource{})
			setting.Source, setting.SourcePath = scope, path
			setting.Note = "project default_model was unresolved; user/default value retained"
		}
		settings = append(settings, setting)
	}
	return settings
}

func settingSpec(path string, value any) configSettingSpec {
	return derivedSettingSpec(path, value)
}

func derivedSettingSpec(path string, value any, fallbacks ...string) configSettingSpec {
	candidates := [][]string{strings.Split(path, ".")}
	for _, fallback := range fallbacks {
		candidates = append(candidates, strings.Split(fallback, "."))
	}
	return configSettingSpec{path: path, value: value, candidates: candidates}
}

func configSettingSource(candidates [][]string, user, project inspectedConfigSource) (string, string) {
	for _, candidate := range candidates {
		if project.report.Status == ConfigSourceValid && project.meta.IsDefined(candidate...) {
			return ConfigSourceProject, project.report.Path
		}
		if user.report.Status == ConfigSourceValid && user.meta.IsDefined(candidate...) {
			return ConfigSourceUser, user.report.Path
		}
	}
	return ConfigSourceBuiltin, ""
}

func runtimeConfigDiagnostics(cfg *Config, project inspectedConfigSource) []ConfigDiagnostic {
	var diagnostics []ConfigDiagnostic
	if ignored := cfg.IgnoredProjectDefaultModel(); ignored != "" {
		diagnostics = append(diagnostics, ConfigDiagnostic{
			Level:      "warning",
			Code:       "unresolved_project_model",
			Scope:      ConfigSourceProject,
			Path:       "default_model",
			SourcePath: project.report.Path,
			Message:    "project default_model does not resolve; the user/default model remains effective",
		})
	}
	return diagnostics
}

func appendUniqueString(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func sortConfigDiagnostics(diagnostics []ConfigDiagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left := diagnostics[i].Scope + "\x00" + diagnostics[i].Path + "\x00" + diagnostics[i].Code
		right := diagnostics[j].Scope + "\x00" + diagnostics[j].Path + "\x00" + diagnostics[j].Code
		return left < right
	})
}

// RenderConfigInspectionText renders the same redacted contract as JSON.
func RenderConfigInspectionText(report ConfigInspection) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Reasonix effective config\n  root: %s\n  valid: %t\n", report.Root, report.Valid)
	b.WriteString("Sources:\n")
	for _, source := range report.Sources {
		fmt.Fprintf(&b, "  %-7s %-7s", source.Scope, source.Status)
		if source.Loaded {
			b.WriteString(" loaded")
		}
		if source.Path != "" {
			fmt.Fprintf(&b, "  %s", source.Path)
		}
		b.WriteByte('\n')
	}
	if report.EffectiveAvailable {
		b.WriteString("Effective settings:\n")
		for _, setting := range report.Settings {
			fmt.Fprintf(&b, "  %s = %s  [%s", setting.Path, inspectionValue(setting.Value), setting.Source)
			if setting.SourcePath != "" {
				fmt.Fprintf(&b, ": %s", setting.SourcePath)
			}
			b.WriteString("]")
			if setting.Note != "" {
				fmt.Fprintf(&b, "  %s", setting.Note)
			}
			b.WriteByte('\n')
		}
	} else {
		b.WriteString("Effective settings: unavailable\n")
	}
	if len(report.Diagnostics) > 0 {
		b.WriteString("Diagnostics:\n")
		for _, diagnostic := range report.Diagnostics {
			fmt.Fprintf(&b, "  %s %s %s (%s): %s\n", diagnostic.Level, diagnostic.Code, diagnostic.Path, diagnostic.Scope, diagnostic.Message)
		}
	}
	if len(report.Warnings) > 0 {
		b.WriteString("Warnings:\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(&b, "  %s\n", warning)
		}
	}
	if len(report.Errors) > 0 {
		b.WriteString("Errors:\n")
		for _, err := range report.Errors {
			fmt.Fprintf(&b, "  %s\n", err)
		}
	}
	return b.String()
}

func inspectionValue(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(raw)
}
