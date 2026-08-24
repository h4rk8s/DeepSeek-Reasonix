package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestBuildWriterSkillUsesRepoLocalWorktree(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("run-skill-worktree",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "skill-1", Name: "run_skill", Arguments: `{"name":"wskill","arguments":"inspect"}`}}},
		testutil.Turn{Text: "child done"},
		testutil.Turn{Text: "parent done"},
	)
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
completion_validation = "off"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	writeFile(t, dir, ".reasonix/skills/wskill.md",
		"---\ndescription: isolated writer\nrunAs: subagent\nallowed-tools: read_file\nisolation: worktree\n---\nwriter body")
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "reasonix@example.test"},
		{"config", "user.name", "Reasonix Test"},
		{"add", "."},
		{"commit", "-m", "seed"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ctrl.Run(context.Background(), "run isolated skill"); err != nil {
		ctrl.Close()
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()

	entries, err := os.ReadDir(filepath.Join(dir, ".worktree"))
	if err != nil {
		t.Fatalf("read repo-local worktrees: %v", err)
	}
	var worktrees []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".reasonix" {
			worktrees = append(worktrees, entry.Name())
		}
	}
	if len(worktrees) != 1 || !strings.HasPrefix(worktrees[0], "reasonix-wskill-") {
		t.Fatalf("repo-local worktrees = %v, want one wskill allocation", worktrees)
	}
	status := exec.Command("git", "-C", dir, "status", "--porcelain=v1", "--untracked-files=all")
	out, err := status.CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("parent workspace status = %q, %v; want clean", out, err)
	}
}
