package agent

import (
	"context"
	"fmt"

	"reasonix/internal/worktree"
)

func (t *TaskTool) resolveProfileIsolation(ctx context.Context, spec ProfileExecSpec) (SubagentIsolation, string, worktree.Resource, error) {
	isolation, err := normalizeSubagentIsolation(spec.Grant.Isolation)
	if err != nil {
		return "", "", worktree.Resource{}, err
	}
	if spec.Grant.ReadOnly && isolation != SubagentIsolationNone {
		return "", "", worktree.Resource{}, fmt.Errorf("isolation is not valid for read-only tasks")
	}
	name := firstNonEmpty(spec.Task.Description, spec.Worker.Name, "task")
	root, resource, err := t.resolveSubagentWorkspace(ctx, isolation, spec.Context.ContinueFrom, spec.Context.ForkFrom, name)
	if err != nil {
		return "", "", worktree.Resource{}, err
	}
	return subagentIsolationFromResource(isolation, resource), root, resource, nil
}
