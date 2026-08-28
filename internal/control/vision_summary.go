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
	prepared, _ := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences)
	if c == nil || (len(images) == 0 && len(prepared.inputs) == 0) || c.imageInputEnabled() {
		return input, ctx, nil
	}
	if c.visionModel == "" && !prepared.requiresImageUnderstanding {
		localInput, used, err := c.tryLocalImageUnderstanding(ctx, input, sourceInputs...)
		if err != nil {
			return input, ctx, fmt.Errorf("图片理解失败，当前回答尚未发送：%w", err)
		}
		if used {
			return localInput, ctx, nil
		}
		// Keep the frozen bytes available to vision-capable child agents while
		// withholding them from the text-only parent provider request.
		return input, agent.WithUserImageInputs(ctx, nil), nil
	}
	var svc *imageinput.Service
	var history func() []provider.Message
	if c.executor != nil {
		svc = c.executor.ImageInput()
		history = c.executor.Session().Snapshot
	}
	if svc == nil {
		svc = imageinput.New(imageinput.Config{Model: c.visionModel, Resolve: c.visionProviderResolver, Select: c.visionModelSelector})
	}
	target, err := svc.SelectModel(c.selection.ref, images)
	if err != nil {
		return c.fallbackVisionTurn(ctx, input, err, sourceInputs...)
	}
	if len(prepared.inputs) > 0 {
		route, routeErr := c.imageRequestRoute(target)
		if routeErr != nil {
			return input, ctx, routeErr
		}
		images, err = c.resolveImageInputsForRoute(ctx, prepared.inputs, route)
		if err != nil {
			return input, ctx, err
		}
	}
	summary, err := svc.UnderstandSelected(ctx, target, images, history, c.sink)
	if err != nil {
		return c.fallbackVisionTurn(ctx, input, err, sourceInputs...)
	}
	return imageinput.AppendSummary(input, summary), agent.WithVisionSummary(ctx, summary), nil
}

func (c *Controller) fallbackVisionTurn(ctx context.Context, input string, modelErr error, sourceInputs ...string) (string, context.Context, error) {
	localInput, used, localErr := c.tryLocalImageUnderstanding(ctx, input, sourceInputs...)
	if used {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "vision model failed; used local image fallback: " + modelErr.Error()})
		return localInput, ctx, nil
	}
	if localErr != nil {
		return input, ctx, fmt.Errorf("图片理解失败，当前回答尚未发送：%w", errors.Join(
			fmt.Errorf("model: %w", modelErr),
			fmt.Errorf("local fallback: %w", localErr),
		))
	}
	return input, ctx, fmt.Errorf("图片理解失败，当前回答尚未发送：%w", modelErr)
}
