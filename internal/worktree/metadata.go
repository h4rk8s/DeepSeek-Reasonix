package worktree

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/fileutil"
)

const (
	mergeMetadataVersion = 1
	mergeMetadataName    = "metadata.json"
)

type mergeMetadata struct {
	Version        int    `json:"version"`
	Kind           Kind   `json:"kind,omitempty"`
	SourceRoot     string `json:"sourceRoot"`
	TargetBranch   string `json:"targetBranch"`
	CreatedHead    string `json:"createdHead"`
	WorktreeRoot   string `json:"worktreeRoot"`
	WorktreeBranch string `json:"worktreeBranch"`
}

type managedPathLayout uint8

const (
	managedPathLegacy managedPathLayout = iota
	managedPathRepoLocal
)

func writeResourceMergeMetadata(resource Resource) error {
	kind := resource.Kind
	if kind == "" {
		kind = KindDelivery
	}
	metadataKind := kind
	if metadataKind == KindDelivery {
		metadataKind = ""
	}
	metadata := mergeMetadata{
		Version:        mergeMetadataVersion,
		Kind:           metadataKind,
		SourceRoot:     filepath.Clean(resource.SourceRoot),
		TargetBranch:   strings.TrimSpace(resource.SourceBranch),
		CreatedHead:    strings.TrimSpace(resource.BaseCommit),
		WorktreeRoot:   filepath.Clean(resource.WorktreeRoot),
		WorktreeBranch: strings.TrimSpace(resource.Branch),
	}
	body, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode worktree metadata: %w", err)
	}
	body = append(body, '\n')
	path := metadataPathFor(metadata)
	if metadata.Kind == KindSubagent {
		if err := ensureRealDirectory(filepath.Join(filepath.Dir(metadata.WorktreeRoot), ".reasonix")); err != nil {
			return fmt.Errorf("prepare repo-local metadata storage: %w", err)
		}
		if err := ensureRealDirectory(filepath.Dir(path)); err != nil {
			return fmt.Errorf("prepare worktree metadata storage: %w", err)
		}
	}
	if err := fileutil.AtomicCreateFile(path, body, 0o600); err != nil {
		return fmt.Errorf("publish worktree metadata: %w", err)
	}
	return nil
}

func metadataPath(worktreeRoot string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(worktreeRoot)), mergeMetadataName)
}

func metadataPathFor(metadata mergeMetadata) string {
	if metadata.Kind == KindSubagent {
		return filepath.Join(repoLocalControlDir(metadata.WorktreeRoot), mergeMetadataName)
	}
	return metadataPath(metadata.WorktreeRoot)
}

func repoLocalControlDir(worktreeRoot string) string {
	root := filepath.Clean(worktreeRoot)
	return filepath.Join(filepath.Dir(root), ".reasonix", filepath.Base(root))
}

func mergeControlDir(metadata mergeMetadata) string {
	if metadata.Kind == KindSubagent {
		return repoLocalControlDir(metadata.WorktreeRoot)
	}
	return filepath.Dir(metadata.WorktreeRoot)
}

func readMergeMetadata(worktreeRoot, managedRoot string) (mergeMetadata, string, error) {
	root, layout, err := validateManagedWorktreePath(worktreeRoot, managedRoot)
	if err != nil {
		return mergeMetadata{}, "", err
	}
	path, err := metadataPathForLayout(root, layout)
	if err != nil {
		return mergeMetadata{}, "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return mergeMetadata{}, path, errors.New("this worktree predates safe Merge-Back metadata; merge it manually from its source checkout")
	}
	if err != nil {
		return mergeMetadata{}, path, fmt.Errorf("inspect worktree metadata: %w", err)
	}
	if !info.Mode().IsRegular() {
		return mergeMetadata{}, path, errors.New("worktree metadata is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return mergeMetadata{}, path, errors.New("worktree metadata permissions are too broad")
	}
	metadata, err := decodeMergeMetadata(path)
	if err != nil {
		return mergeMetadata{}, path, err
	}
	if err := sameDirectory(root, metadata.WorktreeRoot); err != nil {
		return mergeMetadata{}, path, fmt.Errorf("worktree metadata root mismatch: %w", err)
	}
	metadata.WorktreeRoot = root
	metadata.SourceRoot = filepath.Clean(metadata.SourceRoot)
	metadata.TargetBranch = strings.TrimSpace(metadata.TargetBranch)
	metadata.CreatedHead = strings.TrimSpace(metadata.CreatedHead)
	metadata.WorktreeBranch = strings.TrimSpace(metadata.WorktreeBranch)
	if layout == managedPathRepoLocal && metadata.Kind != KindSubagent {
		return mergeMetadata{}, path, errors.New("repo-local worktree metadata is not a subagent resource")
	}
	return metadata, path, nil
}

func readMergeMetadataForCleanup(worktreeRoot, managedRoot string) (mergeMetadata, string, bool, error) {
	rootExists := false
	if _, err := os.Lstat(worktreeRoot); err == nil {
		rootExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return mergeMetadata{}, "", false, fmt.Errorf("inspect cleanup worktree: %w", err)
	}
	absManaged, err := filepath.Abs(strings.TrimSpace(managedRoot))
	if err != nil {
		return mergeMetadata{}, "", false, fmt.Errorf("resolve managed worktree storage: %w", err)
	}
	realManaged, err := filepath.EvalSymlinks(absManaged)
	if err != nil {
		return mergeMetadata{}, "", false, fmt.Errorf("resolve managed worktree storage links: %w", err)
	}
	realRoot, err := resolveMissingCleanupPath(worktreeRoot)
	if err != nil {
		return mergeMetadata{}, "", false, err
	}
	rel, err := filepath.Rel(filepath.Clean(realManaged), filepath.Clean(realRoot))
	layout, err := classifyManagedPath(realManaged, rel, err)
	if err != nil {
		return mergeMetadata{}, "", false, errors.New("cleanup worktree does not match the managed allocation layout")
	}
	allocationDir := filepath.Dir(realRoot)
	if layout == managedPathRepoLocal {
		allocationDir = repoLocalControlDir(realRoot)
	}
	realAllocation, err := filepath.EvalSymlinks(allocationDir)
	if err != nil {
		return mergeMetadata{}, "", false, fmt.Errorf("resolve cleanup allocation links: %w", err)
	}
	realRel, err := filepath.Rel(filepath.Clean(realManaged), filepath.Clean(realAllocation))
	if err != nil || realRel == "." || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		return mergeMetadata{}, "", false, errors.New("cleanup allocation resolves outside managed storage")
	}
	path := filepath.Join(realAllocation, mergeMetadataName)
	metadata, err := decodeMergeMetadata(path)
	if err != nil {
		return mergeMetadata{}, path, false, err
	}
	realMetadataRoot, err := resolveMissingCleanupPath(metadata.WorktreeRoot)
	if err != nil {
		return mergeMetadata{}, path, false, fmt.Errorf("resolve cleanup metadata root: %w", err)
	}
	if filepath.Clean(realMetadataRoot) != filepath.Clean(realRoot) {
		return mergeMetadata{}, path, false, errors.New("cleanup metadata root mismatch")
	}
	metadata.WorktreeRoot = filepath.Clean(realRoot)
	metadata.SourceRoot = filepath.Clean(metadata.SourceRoot)
	metadata.TargetBranch = strings.TrimSpace(metadata.TargetBranch)
	metadata.CreatedHead = strings.TrimSpace(metadata.CreatedHead)
	metadata.WorktreeBranch = strings.TrimSpace(metadata.WorktreeBranch)
	if layout == managedPathRepoLocal && metadata.Kind != KindSubagent {
		return mergeMetadata{}, path, false, errors.New("repo-local cleanup metadata is not a subagent resource")
	}
	return metadata, path, rootExists, nil
}

func resolveMissingCleanupPath(path string) (string, error) {
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("resolve cleanup worktree: %w", err)
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(absPath))
	if err != nil {
		return "", fmt.Errorf("resolve cleanup allocation links: %w", err)
	}
	return filepath.Join(realParent, filepath.Base(absPath)), nil
}

func decodeMergeMetadata(path string) (mergeMetadata, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return mergeMetadata{}, fmt.Errorf("inspect worktree metadata: %w", err)
	}
	if !info.Mode().IsRegular() {
		return mergeMetadata{}, errors.New("worktree metadata is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return mergeMetadata{}, errors.New("worktree metadata permissions are too broad")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return mergeMetadata{}, fmt.Errorf("read worktree metadata: %w", err)
	}
	var metadata mergeMetadata
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return mergeMetadata{}, fmt.Errorf("decode worktree metadata: %w", err)
	}
	if metadata.Version != mergeMetadataVersion {
		return mergeMetadata{}, fmt.Errorf("unsupported worktree metadata version %d", metadata.Version)
	}
	if metadata.Kind == "" {
		metadata.Kind = KindDelivery
	}
	if strings.TrimSpace(metadata.SourceRoot) == "" || strings.TrimSpace(metadata.CreatedHead) == "" ||
		strings.TrimSpace(metadata.WorktreeRoot) == "" || strings.TrimSpace(metadata.WorktreeBranch) == "" {
		return mergeMetadata{}, errors.New("worktree metadata is incomplete")
	}
	branchKind, ok := managedBranchKind(metadata.WorktreeBranch)
	if !ok || branchKind != metadata.Kind {
		return mergeMetadata{}, errors.New("worktree metadata names an unmanaged branch")
	}
	return metadata, nil
}

func validateManagedWorktreePath(worktreeRoot, managedRoot string) (string, managedPathLayout, error) {
	worktreeRoot = strings.TrimSpace(worktreeRoot)
	managedRoot = strings.TrimSpace(managedRoot)
	if worktreeRoot == "" || managedRoot == "" {
		return "", 0, errors.New("managed worktree identity is incomplete")
	}
	absManaged, err := filepath.Abs(managedRoot)
	if err != nil {
		return "", 0, fmt.Errorf("resolve managed worktree storage: %w", err)
	}
	absRoot, err := filepath.Abs(worktreeRoot)
	if err != nil {
		return "", 0, fmt.Errorf("resolve worktree root: %w", err)
	}
	realManaged, err := filepath.EvalSymlinks(absManaged)
	if err != nil {
		return "", 0, fmt.Errorf("resolve managed worktree storage links: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", 0, fmt.Errorf("resolve worktree links: %w", err)
	}
	realRel, err := filepath.Rel(filepath.Clean(realManaged), filepath.Clean(realRoot))
	layout, err := classifyManagedPath(realManaged, realRel, err)
	if err != nil {
		return "", 0, err
	}
	return filepath.Clean(realRoot), layout, nil
}

func classifyManagedPath(managedRoot, rel string, relErr error) (managedPathLayout, error) {
	if relErr != nil || rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return 0, errors.New("worktree resolves outside Reasonix-managed storage")
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if len(parts) == 1 && parts[0] != "" && filepath.Base(filepath.Clean(managedRoot)) == repoLocalWorktreeDir {
		return managedPathRepoLocal, nil
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		return managedPathLegacy, nil
	}
	return 0, errors.New("worktree does not match the managed allocation layout")
}

func metadataPathForLayout(worktreeRoot string, layout managedPathLayout) (string, error) {
	if layout != managedPathRepoLocal {
		return metadataPath(worktreeRoot), nil
	}
	controlRoot := filepath.Join(filepath.Dir(worktreeRoot), ".reasonix")
	if info, err := os.Lstat(controlRoot); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("repo-local metadata root is not a real directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect repo-local metadata root: %w", err)
	}
	controlDir := repoLocalControlDir(worktreeRoot)
	if info, err := os.Lstat(controlDir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("worktree metadata storage is not a real directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect worktree metadata storage: %w", err)
	}
	return filepath.Join(controlDir, mergeMetadataName), nil
}

func sameDirectory(left, right string) error {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return err
	}
	if !leftInfo.IsDir() || !rightInfo.IsDir() || !os.SameFile(leftInfo, rightInfo) {
		return errors.New("directories are not identical")
	}
	return nil
}

func verifyRollbackMetadata(sourceRoot, worktreeRoot, branch, head string) (string, error) {
	kind, ok := managedBranchKind(branch)
	if !ok {
		return "", fmt.Errorf("rollback branch %q is unmanaged", branch)
	}
	path := metadataPathFor(mergeMetadata{Kind: kind, WorktreeRoot: worktreeRoot})
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path, nil
	} else if err != nil {
		return path, fmt.Errorf("inspect rollback metadata: %w", err)
	}
	metadata, err := decodeMergeMetadata(path)
	if err != nil || metadata.WorktreeBranch != branch || metadata.CreatedHead != head {
		return path, errors.New("rollback worktree metadata changed; the worktree was preserved")
	}
	if err := sameDirectory(metadata.SourceRoot, sourceRoot); err != nil {
		return path, errors.New("rollback source metadata changed; the worktree was preserved")
	}
	return path, nil
}

func managedBranchKind(branch string) (Kind, bool) {
	switch {
	case strings.HasPrefix(branch, "reasonix/delivery-"):
		return KindDelivery, true
	case strings.HasPrefix(branch, "reasonix/subagent-"):
		return KindSubagent, true
	default:
		return "", false
	}
}

func removeResourceControlMetadata(resource Resource) error {
	kind := resource.Kind
	if kind == "" {
		kind = KindDelivery
	}
	metadata := mergeMetadata{Kind: kind, WorktreeRoot: resource.WorktreeRoot}
	path := metadataPathFor(metadata)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove worktree merge metadata: %w", err)
	}
	if kind != KindSubagent {
		return nil
	}
	controlDir := mergeControlDir(metadata)
	if err := os.Remove(filepath.Join(controlDir, cleanupStateName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove worktree cleanup state: %w", err)
	}
	_ = os.Remove(controlDir)
	return nil
}
