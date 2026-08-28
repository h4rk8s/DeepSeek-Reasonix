package boot

import (
	"context"

	"reasonix/internal/worktree"
)

func createSkillSubagentWorkspace(ctx context.Context, manager *worktree.Manager, root, taskName string) (string, worktree.Resource, error) {
	resource, err := manager.Create(ctx, root, worktree.CreatePolicy{
		Kind:        worktree.KindSubagent,
		TaskName:    taskName,
		DirtyPolicy: worktree.DirtyPolicyReject,
	})
	if err != nil {
		return "", worktree.Resource{}, err
	}
	return resource.WorkspaceRoot, resource, nil
}
