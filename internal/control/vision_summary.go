package control

import (
	"context"
	"errors"
	"fmt"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
)

// prepareVisionTurn shares the same per-session processor as tool results.
func (c *Controller) prepareVisionTurn(ctx context.Context, input string, images []string, sourceInputs ...string) (string, context.Context, error) {
	if c == nil || len(images) == 0 || c.imageInputEnabled() {
		return input, ctx, nil
	}
	var modelErr error
	if c.visionModel != "" {
		var svc *imageinput.Service
		var history func() []provider.Message
		if c.executor != nil {
			svc = c.executor.ImageInput()
			history = c.executor.Session().Snapshot
		}
		if svc == nil {
			svc = imageinput.New(imageinput.Config{Model: c.visionModel, Resolve: c.visionProviderResolver, Select: c.visionModelSelector})
		}
		summary, err := svc.Understand(ctx, c.selection.ref, images, history, c.sink)
		if err == nil {
			return imageinput.AppendSummary(input, summary), agent.WithVisionSummary(ctx, summary), nil
		}
		modelErr = err
	}
	localInput, used, localErr := c.tryLocalImageUnderstanding(ctx, input, sourceInputs...)
	if used {
		if modelErr != nil {
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "vision model failed; used local image fallback: " + modelErr.Error()})
		}
		return localInput, ctx, nil
	}
	if modelErr != nil && localErr != nil {
		return input, ctx, fmt.Errorf("图片理解失败，当前回答尚未发送：%w", errors.Join(
			fmt.Errorf("model: %w", modelErr),
			fmt.Errorf("local fallback: %w", localErr),
		))
	}
	if modelErr != nil {
		return input, ctx, fmt.Errorf("图片理解失败，当前回答尚未发送：%w", modelErr)
	}
	if localErr != nil {
		return input, ctx, fmt.Errorf("图片理解失败，当前回答尚未发送：%w", localErr)
	}
	return input, ctx, nil
}
