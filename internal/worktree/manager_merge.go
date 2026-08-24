package worktree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// InspectMerge returns the exact merge identities for a managed resource.
func (m *Manager) InspectMerge(ctx context.Context, resource Resource) (MergeInspection, error) {
	resource, err := m.canonicalResource(ctx, resource)
	if err != nil {
		return emptyMergeInspection(), err
	}
	if resource.CleanupState == CleanupStateRetained || resource.CleanupState == CleanupStateRemoved {
		return unavailableInspection(emptyMergeInspection(), "managed worktree merge lifecycle is already finalized")
	}
	managedRoot, err := m.mergeRootForResource(resource)
	if err != nil {
		return emptyMergeInspection(), err
	}
	return InspectMerge(ctx, resourceCheckoutRoot(resource), managedRoot)
}

// MergeBack uses the package exact-tree/CAS lifecycle and never finalizes the
// child worktree. Dirty child content requires request.AutoCommitDirty.
func (m *Manager) MergeBack(ctx context.Context, resource Resource, request MergeRequest) (MergeResult, error) {
	resource, err := m.canonicalResource(ctx, resource)
	if err != nil {
		return MergeResult{}, err
	}
	if resource.CleanupState == CleanupStateRetained || resource.CleanupState == CleanupStateRemoved {
		return MergeResult{}, errors.New("managed worktree merge lifecycle is already finalized")
	}
	if err := bindMergeWorkspace(&request, resource); err != nil {
		return MergeResult{}, err
	}
	managedRoot, err := m.mergeRootForResource(resource)
	if err != nil {
		return MergeResult{}, err
	}
	result, mergeErr := MergeBack(ctx, managedRoot, request)
	changed := false
	if strings.TrimSpace(result.WorktreeHead) != "" && result.WorktreeHead != resource.HeadCommit {
		resource.HeadCommit = result.WorktreeHead
		changed = true
	}
	if result.Merged && resource.LifecycleState != LifecycleStateApplied {
		resource.LifecycleState = LifecycleStateApplied
		changed = true
	}
	if changed {
		if writeErr := m.writeResource(resource); writeErr != nil {
			return result, errors.Join(mergeErr, fmt.Errorf("persist merged worktree resource: %w", writeErr))
		}
	}
	return result, mergeErr
}

// FinalizeMerge explicitly advances cleanup after a successful merge. It keeps
// the upstream retained-recovery behavior and never runs from MergeBack.
func (m *Manager) FinalizeMerge(ctx context.Context, resource Resource, request CleanupRequest) (CleanupResult, error) {
	resource, err := m.canonicalResource(ctx, resource)
	if err != nil {
		return CleanupResult{}, err
	}
	if strings.TrimSpace(request.WorktreeRoot) == "" {
		request.WorktreeRoot = resource.WorktreeRoot
	} else if !sameCleanupPath(request.WorktreeRoot, resource.WorktreeRoot) {
		return CleanupResult{}, errors.New("cleanup worktree does not match the managed resource")
	}
	managedRoot, err := m.mergeRootForResource(resource)
	if err != nil {
		return CleanupResult{}, err
	}
	result, cleanupErr := FinalizeMerge(ctx, managedRoot, request)
	changed := false
	if result.RecoveryRetained && strings.TrimSpace(result.RecoveryRoot) != "" {
		resource.RecoveryRoot = result.RecoveryRoot
		resource.CleanupState = CleanupStateRetained
		changed = true
	}
	if result.Completed {
		resource.LifecycleState = LifecycleStateRemoved
		resource.CleanupState = CleanupStateRemoved
		changed = true
	}
	if changed {
		if writeErr := m.writeResource(resource); writeErr != nil {
			return result, errors.Join(cleanupErr, fmt.Errorf("persist finalized worktree resource: %w", writeErr))
		}
	}
	return result, cleanupErr
}

func (m *Manager) canonicalResource(ctx context.Context, resource Resource) (Resource, error) {
	if strings.TrimSpace(resource.IsolationID) == "" {
		return Resource{}, errors.New("worktree isolation id is required")
	}
	return m.Show(ctx, resource.IsolationID)
}

func (m *Manager) mergeRootForResource(resource Resource) (string, error) {
	managedRoot, err := m.managedRootOrError()
	if err != nil {
		return "", err
	}
	if resource.Kind != KindSubagent {
		return managedRoot, nil
	}
	repoLocalRoot := filepath.Join(resource.SourceRoot, repoLocalWorktreeDir)
	if sameCleanupPath(filepath.Dir(resource.WorktreeRoot), repoLocalRoot) {
		return repoLocalRoot, nil
	}
	return managedRoot, nil
}

func bindMergeWorkspace(request *MergeRequest, resource Resource) error {
	if request == nil {
		return errors.New("merge request is required")
	}
	checkoutRoot := resourceCheckoutRoot(resource)
	if strings.TrimSpace(request.WorkspaceRoot) == "" {
		request.WorkspaceRoot = checkoutRoot
		return nil
	}
	if !sameCleanupPath(request.WorkspaceRoot, resource.WorkspaceRoot) &&
		!sameCleanupPath(request.WorkspaceRoot, resource.WorktreeRoot) &&
		!sameCleanupPath(request.WorkspaceRoot, resource.RecoveryRoot) &&
		!sameCleanupPath(request.WorkspaceRoot, checkoutRoot) {
		return errors.New("merge workspace does not match the managed resource")
	}
	request.WorkspaceRoot = checkoutRoot
	return nil
}
