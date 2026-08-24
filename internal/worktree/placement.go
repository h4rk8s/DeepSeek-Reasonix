package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

const repoLocalWorktreeDir = ".worktree"

var excludeMutationMu sync.Mutex

func worktreeAllocation(ctx context.Context, info inspection, managedRoot, id string, policy CreatePolicy) (string, string, error) {
	if policy.Kind != KindSubagent {
		repoKey := repoStorageKey(info.commonDir)
		repoBase := safePathComponent(filepath.Base(info.RepoRoot))
		if repoBase == "" {
			repoBase = "repository"
		}
		return managedRoot, filepath.Join(managedRoot, repoKey, id, repoBase), nil
	}

	allocationRoot, err := prepareRepoLocalWorktreeRoot(ctx, info)
	if err != nil {
		return "", "", err
	}
	return allocationRoot, filepath.Join(allocationRoot, subagentWorktreeName(policy.TaskName, id)), nil
}

func prepareRepoLocalWorktreeRoot(ctx context.Context, info inspection) (string, error) {
	if err := ensureRepoLocalWorktreeIgnored(ctx, info); err != nil {
		return "", err
	}
	root := filepath.Join(info.RepoRoot, repoLocalWorktreeDir)
	if err := ensureRealDirectory(root); err != nil {
		return "", fmt.Errorf("prepare repo-local worktree storage: %w", err)
	}
	return root, nil
}

func ensureRepoLocalWorktreeIgnored(ctx context.Context, info inspection) error {
	excludeMutationMu.Lock()
	defer excludeMutationMu.Unlock()

	tracked, stderr, err := runGit(ctx, info.RepoRoot, "ls-files", "--", repoLocalWorktreeDir)
	if err != nil {
		return fmt.Errorf("inspect tracked .worktree paths: %w%s", err, stderrSuffix(stderr))
	}
	if strings.TrimSpace(tracked) != "" {
		return errors.New("the repository tracks .worktree content; refusing to use it for subagent isolation")
	}
	ignored, err := repoLocalWorktreeIgnored(ctx, info.RepoRoot)
	if err != nil {
		return err
	}
	if ignored {
		return nil
	}
	excludePath := filepath.Join(info.commonDir, "info", "exclude")
	if err := ensureRealDirectory(filepath.Dir(excludePath)); err != nil {
		return fmt.Errorf("prepare Git exclude directory: %w", err)
	}
	if excludeInfo, statErr := os.Lstat(excludePath); statErr == nil {
		if !excludeInfo.Mode().IsRegular() || excludeInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("Git exclude path is not a regular file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect Git exclude file: %w", statErr)
	}
	existing, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read Git exclude file: %w", err)
	}
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	file, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open Git exclude file: %w", err)
	}
	_, writeErr := file.WriteString(prefix + "/.worktree/\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("record repo-local worktree exclusion: %w", errors.Join(writeErr, closeErr))
	}
	ignored, err = repoLocalWorktreeIgnored(ctx, info.RepoRoot)
	if err != nil {
		return err
	}
	if !ignored {
		return errors.New("Git did not honor the repo-local .worktree exclusion")
	}
	return nil
}

func repoLocalWorktreeIgnored(ctx context.Context, repoRoot string) (bool, error) {
	_, stderr, err := runGit(ctx, repoRoot, "check-ignore", "-q", "--no-index", "--", ".worktree/reasonix-probe")
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, fmt.Errorf("inspect repo-local worktree exclusion: %w%s", err, stderrSuffix(stderr))
}

func ensureRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a real directory")
	}
	return nil
}

func subagentWorktreeName(taskName, id string) string {
	name := strings.Join(strings.Fields(strings.TrimSpace(taskName)), "-")
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	name = strings.Trim(name, "-_. ")
	runes := []rune(name)
	if len(runes) > 40 {
		name = string(runes[:40])
		name = strings.TrimRight(name, "-_. ")
	}
	if name == "" {
		name = "task"
	}
	return "reasonix-" + name + "-" + id
}
