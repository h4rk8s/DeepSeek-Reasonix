package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func TestTaskToolSchemaHasBackgroundButNoIsolationContractYet(t *testing.T) {
	schema := string((&TaskTool{}).Schema())
	if !strings.Contains(schema, `"run_in_background"`) {
		t.Fatalf("task schema no longer exposes run_in_background:\n%s", schema)
	}
	if strings.Contains(schema, `"isolation"`) {
		t.Fatalf("task schema unexpectedly exposes isolation before the contract is implemented:\n%s", schema)
	}
}

func TestTaskToolPersistedSubagentRunsShareParentWorkspaceToday(t *testing.T) {
	store := NewSubagentStore(t.TempDir())
	parentWorkspace := t.TempDir()
	task := (&TaskTool{sysPrompt: "system"}).WithTranscripts(store, parentWorkspace, "base-model", "base-effort")
	reg := tool.NewRegistry()

	run1, err := task.prepareTranscriptRun(reg, "", "", "parent-session", "call-1", "", "")
	if err != nil {
		t.Fatalf("prepare run1: %v", err)
	}
	defer run1.Release()
	run2, err := task.prepareTranscriptRun(reg, "", "", "parent-session", "call-2", "", "")
	if err != nil {
		t.Fatalf("prepare run2: %v", err)
	}
	defer run2.Release()

	if run1.Meta.WorkspaceRoot != parentWorkspace || run2.Meta.WorkspaceRoot != parentWorkspace {
		t.Fatalf("runs should share parent workspace today: run1=%q run2=%q parent=%q", run1.Meta.WorkspaceRoot, run2.Meta.WorkspaceRoot, parentWorkspace)
	}
	if run1.Meta.WorkspaceRoot != run2.Meta.WorkspaceRoot {
		t.Fatalf("runs should not be isolated today: run1=%q run2=%q", run1.Meta.WorkspaceRoot, run2.Meta.WorkspaceRoot)
	}

	raw, err := json.Marshal(run1.Meta)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"isolation", "isolationId", "isolation_id", "worktreeRoot", "worktree_root", "sourceRoot", "source_root"} {
		if _, ok := meta[key]; ok {
			t.Fatalf("subagent metadata unexpectedly has %q before worktree isolation is implemented: %s", key, string(raw))
		}
	}
}
