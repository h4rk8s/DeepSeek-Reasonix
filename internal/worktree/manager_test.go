package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerRejectsDirtySourceForSubagentWorktree(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(t.TempDir())
	_, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if !errors.Is(err, ErrDirtySource) {
		t.Fatalf("Create err = %v, want ErrDirtySource", err)
	}
	list, listErr := mgr.List(ctx)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(list) != 0 {
		t.Fatalf("dirty source should not create resources, got %d", len(list))
	}
}

func TestManagerRejectsTrackedWorktreeNamespace(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	reserved := filepath.Join(repo, repoLocalWorktreeDir, "reserved.txt")
	if err := os.MkdirAll(filepath.Dir(reserved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reserved, []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", ".worktree/reserved.txt")
	runGitTest(t, repo, "commit", "-m", "reserve worktree namespace")

	manager := NewManager(t.TempDir())
	if _, err := manager.Create(ctx, repo, CreatePolicy{Kind: KindSubagent}); err == nil || !strings.Contains(err.Error(), "tracks .worktree") {
		t.Fatalf("Create with tracked .worktree = %v, want namespace rejection", err)
	}
	if branches := strings.TrimSpace(runGitTest(t, repo, "branch", "--list", "reasonix/subagent-*")); branches != "" {
		t.Fatalf("rejected allocation created branches: %q", branches)
	}
}

func TestManagerIgnoresPrecreatedWorktreeNamespaceBeforeDirtyCheck(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, repoLocalWorktreeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, repoLocalWorktreeDir, "local-note"), []byte("reserved\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(t.TempDir())
	resource, err := manager.Create(ctx, repo, CreatePolicy{Kind: KindSubagent, TaskName: "existing root"})
	if err != nil {
		t.Fatalf("Create with pre-created .worktree: %v", err)
	}
	if !sameCleanupPath(filepath.Dir(resource.WorktreeRoot), filepath.Join(repo, repoLocalWorktreeDir)) {
		t.Fatalf("worktree root = %q", resource.WorktreeRoot)
	}
	if got := gitStatus(t, repo); got != "" {
		t.Fatalf("source status after namespace reservation = %q", got)
	}
}

func TestManagerCreatesInspectableSubagentResource(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	managed := t.TempDir()
	mgr := NewManager(managed)

	res, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent, TaskName: "inspect cache"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindSubagent {
		t.Fatalf("kind = %q", res.Kind)
	}
	if res.IsolationID == "" {
		t.Fatal("isolation id is empty")
	}
	if !strings.HasPrefix(res.Branch, "reasonix/subagent-") {
		t.Fatalf("branch = %q", res.Branch)
	}
	wantParent := filepath.Join(res.SourceRoot, repoLocalWorktreeDir)
	if parent := filepath.Dir(res.WorktreeRoot); !sameCleanupPath(parent, wantParent) {
		t.Fatalf("worktree parent = %q, want repo-local %q", parent, wantParent)
	}
	if name := filepath.Base(res.WorktreeRoot); !strings.HasPrefix(name, "reasonix-inspect-cache-") {
		t.Fatalf("worktree name = %q, want task-specific prefix", name)
	}
	if res.WorkspaceRoot != res.WorktreeRoot {
		t.Fatalf("workspace root = %q, worktree root = %q", res.WorkspaceRoot, res.WorktreeRoot)
	}
	if res.SourceRoot == res.WorktreeRoot || res.SourceRoot == "" {
		t.Fatalf("source root = %q, worktree root = %q", res.SourceRoot, res.WorktreeRoot)
	}
	if res.BaseCommit == "" || res.HeadCommit == "" || res.BaseCommit != res.HeadCommit {
		t.Fatalf("commits = base %q head %q", res.BaseCommit, res.HeadCommit)
	}
	if res.LifecycleState != LifecycleStateCreated || res.CleanupState != CleanupStateActive || res.CreatedAt.IsZero() {
		t.Fatalf("unexpected lifecycle: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(res.WorktreeRoot, "README.md")); err != nil {
		t.Fatalf("worktree missing committed file: %v", err)
	}
	if got := gitStatus(t, repo); got != "" {
		t.Fatalf("repo-local worktree dirtied source checkout: %q", got)
	}

	reopened := NewManager(managed)
	list, err := reopened.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].IsolationID != res.IsolationID {
		t.Fatalf("List = %+v, want one resource %q", list, res.IsolationID)
	}
	shown, err := reopened.Show(ctx, res.IsolationID)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Branch != res.Branch {
		t.Fatalf("Show branch = %q, want %q", shown.Branch, res.Branch)
	}
	status, err := reopened.Status(ctx, shown)
	if err != nil {
		t.Fatal(err)
	}
	if status.Dirty {
		t.Fatalf("new worktree status dirty: %+v", status)
	}

	if err := os.WriteFile(filepath.Join(res.WorktreeRoot, "README.md"), []byte("child edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err = reopened.Status(ctx, shown)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Dirty || len(status.Paths) != 1 || status.Paths[0] != "README.md" {
		t.Fatalf("dirty status = %+v", status)
	}
	diff, err := reopened.Diff(ctx, shown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "+child edit") || len(diff.Paths) != 1 || diff.Paths[0] != "README.md" {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestManagerCreatesSubagentUnderLinkedSourceWorktree(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	manager := NewManager(t.TempDir())
	primary := initRepo(t)
	linked, err := manager.Create(ctx, primary, CreatePolicy{Kind: KindSubagent, TaskName: "outer"})
	if err != nil {
		t.Fatalf("create linked source: %v", err)
	}
	nested, err := manager.Create(ctx, linked.WorktreeRoot, CreatePolicy{Kind: KindSubagent, TaskName: "nested"})
	if err != nil {
		t.Fatalf("create under linked source: %v", err)
	}
	wantParent := filepath.Join(nested.SourceRoot, repoLocalWorktreeDir)
	if !sameCleanupPath(filepath.Dir(nested.WorktreeRoot), wantParent) {
		t.Fatalf("nested worktree = %q, want direct child of %q", nested.WorktreeRoot, wantParent)
	}
	if got := gitStatus(t, linked.WorktreeRoot); got != "" {
		t.Fatalf("linked source was dirtied by nested worktree: %q", got)
	}
}

func TestManagerMergeBackFromRepositorySubdirectory(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	project := filepath.Join(repo, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "note.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", "project/note.txt")
	runGitTest(t, repo, "commit", "-m", "add project")

	manager := NewManager(t.TempDir())
	resource, err := manager.Create(ctx, project, CreatePolicy{Kind: KindSubagent, TaskName: "subdir"})
	if err != nil {
		t.Fatal(err)
	}
	if resource.WorkspaceRoot == resource.WorktreeRoot {
		t.Fatalf("workspace root = worktree root %q, want selected subdirectory", resource.WorkspaceRoot)
	}
	if err := os.WriteFile(filepath.Join(resource.WorkspaceRoot, "note.txt"), []byte("child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection, err := manager.InspectMerge(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	result, err := manager.MergeBack(ctx, resource, request)
	if err != nil {
		t.Fatalf("MergeBack from subdirectory: %v (%+v)", err, result)
	}
	got, err := os.ReadFile(filepath.Join(project, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "child\n" {
		t.Fatalf("merged subdirectory content = %q, want child", got)
	}
}

func TestManagerMergeBackCleanAndReportExactConflictWithoutPollution(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	mgr := NewManager(t.TempDir())

	first, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.WorktreeRoot, "README.md"), []byte("first writer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection, err := mgr.InspectMerge(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	result, err := mgr.MergeBack(ctx, first, request)
	if err != nil {
		t.Fatalf("first MergeBack: %v (%+v)", err, result)
	}
	if !result.Merged || result.WorktreeHead == "" || result.MergedCommit == "" {
		t.Fatalf("first merge result = %+v", result)
	}
	mergedResource, err := mgr.Show(ctx, first.IsolationID)
	if err != nil {
		t.Fatal(err)
	}
	if mergedResource.LifecycleState != LifecycleStateApplied || mergedResource.HeadCommit != result.WorktreeHead {
		t.Fatalf("merged resource metadata = %+v", mergedResource)
	}
	if _, err := os.Stat(first.WorktreeRoot); err != nil {
		t.Fatalf("MergeBack finalized or removed the child implicitly: %v", err)
	}
	sourceBytes, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceBytes) != "first writer\n" {
		t.Fatalf("source content after clean merge = %q", sourceBytes)
	}
	cleanup, err := mgr.FinalizeMerge(ctx, first, cleanupFromMerge(result))
	if err != nil {
		t.Fatalf("FinalizeMerge: %v (%+v)", err, cleanup)
	}
	if !cleanup.RecoveryRetained || cleanup.RecoveryRoot == "" || filepath.Dir(cleanup.RecoveryRoot) != filepath.Join(first.SourceRoot, repoLocalWorktreeDir) {
		t.Fatalf("repo-local cleanup result = %+v", cleanup)
	}
	if _, err := os.Stat(cleanup.RecoveryRoot); err != nil {
		t.Fatalf("finalization did not retain the recovery worktree: %v", err)
	}
	if _, err := os.Stat(first.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("original worktree path remains after explicit finalization: %v", err)
	}
	retained, err := NewManager(mgr.managedRoot).Show(ctx, first.IsolationID)
	if err != nil {
		t.Fatalf("show finalized resource: %v", err)
	}
	if retained.CleanupState != CleanupStateRetained || retained.RecoveryRoot != cleanup.RecoveryRoot {
		t.Fatalf("finalized resource metadata = %+v", retained)
	}
	postFinalize, err := NewManager(mgr.managedRoot).Status(ctx, retained)
	if err != nil {
		t.Fatalf("inspect retained recovery status: %v", err)
	}
	if postFinalize.Dirty {
		t.Fatalf("retained recovery status = %+v, want clean", postFinalize)
	}
	if inspection, inspectErr := NewManager(mgr.managedRoot).InspectMerge(ctx, retained); inspectErr == nil || inspection.Available {
		t.Fatalf("finalized resource re-entered merge lifecycle: %+v, %v", inspection, inspectErr)
	}

	if err := os.WriteFile(filepath.Join(second.WorktreeRoot, "README.md"), []byte("second writer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection, err = mgr.InspectMerge(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	request = requestFromInspection(inspection)
	request.AutoCommitDirty = true
	result, err = mgr.MergeBack(ctx, second, request)
	if err == nil || result.Merged {
		t.Fatalf("conflicting MergeBack = %+v, %v; want failure", result, err)
	}
	conflict, inspectErr := mgr.InspectMerge(ctx, second)
	if inspectErr != nil {
		t.Fatalf("inspect committed conflict: %v", inspectErr)
	}
	if !conflict.HasConflicts || len(conflict.ConflictFiles) != 1 || conflict.ConflictFiles[0] != "README.md" || !hasBlocker(conflict.Blockers, "merge_conflict") {
		t.Fatalf("conflict inspection = %+v", conflict)
	}
	sourceBytes, err = os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceBytes) != "first writer\n" {
		t.Fatalf("conflicting merge polluted source content = %q", sourceBytes)
	}
	status := gitStatus(t, repo)
	if strings.Contains(status, "UU ") || strings.Contains(status, "<<<<<<<") {
		t.Fatalf("source repo has unresolved conflict state: %q", status)
	}
}

func TestLegacyCreateUsesDeliveryPolicyAndAllowsDirtyCommittedHead(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Create(ctx, repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Branch, "reasonix/delivery-") {
		t.Fatalf("branch = %q", result.Branch)
	}
	if !result.SourceDirty {
		t.Fatalf("SourceDirty = false, want true")
	}
	if _, err := os.Stat(filepath.Join(result.WorktreeRoot, "scratch.txt")); !os.IsNotExist(err) {
		t.Fatalf("dirty source file must not be copied, stat err = %v", err)
	}
}

func TestManagerRemoveRefusesUncommittedAndCommittedChanges(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	mgr := NewManager(t.TempDir())

	dirty, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty.WorktreeRoot, "README.md"), []byte("dirty child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Remove(ctx, dirty); !errors.Is(err, ErrResourceHasChanges) {
		t.Fatalf("Remove dirty err = %v, want ErrResourceHasChanges", err)
	}
	if _, err := os.Stat(dirty.WorktreeRoot); err != nil {
		t.Fatalf("dirty worktree was removed: %v", err)
	}

	committed, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(committed.WorktreeRoot, "README.md"), []byte("committed child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, committed.WorktreeRoot, "add", "README.md")
	runGitTest(t, committed.WorktreeRoot, "commit", "-m", "child commit")
	if _, err := mgr.Remove(ctx, committed); !errors.Is(err, ErrResourceHasChanges) {
		t.Fatalf("Remove committed err = %v, want ErrResourceHasChanges", err)
	}
	if _, err := os.Stat(committed.WorktreeRoot); err != nil {
		t.Fatalf("committed worktree was removed: %v", err)
	}
}

func TestManagerDiscardRequiresExplicitCallAndPersistsRemoval(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	mgr := NewManager(t.TempDir())
	res, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res.WorktreeRoot, "scratch.txt"), []byte("user data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := mgr.Discard(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	if removed.CleanupState != CleanupStateRemoved || removed.LifecycleState != LifecycleStateRemoved {
		t.Fatalf("removed resource = %+v", removed)
	}
	if _, err := os.Stat(res.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("discarded worktree still exists: %v", err)
	}
	shown, err := mgr.Show(ctx, res.IsolationID)
	if err != nil {
		t.Fatal(err)
	}
	if shown.CleanupState != CleanupStateRemoved {
		t.Fatalf("persisted cleanup state = %q", shown.CleanupState)
	}
}

func TestManagerGCReclaimsSafeOrphanAndRetainsCommittedBranch(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	repo := initRepo(t)
	mgr := NewManager(t.TempDir())

	safe, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(safe.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "worktree", "prune")

	committed, err := mgr.Create(ctx, repo, CreatePolicy{Kind: KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(committed.WorktreeRoot, "README.md"), []byte("orphan commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, committed.WorktreeRoot, "add", "README.md")
	runGitTest(t, committed.WorktreeRoot, "commit", "-m", "orphan commit")
	if err := os.RemoveAll(committed.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "worktree", "prune")

	result, err := NewManager(mgr.managedRoot).GC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || result.Removed[0].IsolationID != safe.IsolationID {
		t.Fatalf("GC removed = %+v, want safe orphan", result.Removed)
	}
	if len(result.Blocked) != 1 || result.Blocked[0].IsolationID != committed.IsolationID {
		t.Fatalf("GC blocked = %+v, want committed orphan", result.Blocked)
	}
	if out := runGitTest(t, repo, "branch", "--list", safe.Branch); strings.TrimSpace(out) != "" {
		t.Fatalf("safe orphan branch remains: %q", out)
	}
	if out := runGitTest(t, repo, "branch", "--list", committed.Branch); strings.TrimSpace(out) == "" {
		t.Fatal("committed orphan branch was deleted")
	}
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func gitStatus(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "status", "--porcelain=v1", "--untracked-files=normal")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}
