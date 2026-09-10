package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/sessioninbox"
)

const inboxImageBudgetSafety = int64(4 << 10)

func (c *Controller) freezeInboxReferences(ctx context.Context, submit string, explicit []string) (string, []string, []string) {
	var line strings.Builder
	line.WriteString(submit)
	for _, path := range explicit {
		path = strings.TrimSpace(path)
		if path != "" {
			line.WriteString(" @")
			line.WriteString(EscapeRefPath(path))
		}
	}
	input := line.String()
	if !c.HasRefs(input) {
		return "", nil, nil
	}
	block, errs := c.ResolveRefs(ctx, input)
	images := c.resolveInputImageCandidates(input)
	return block, images, errs
}

func fitInboxEnvelopeImages(env *sessioninbox.PromptEnvelope, maxBytes int64) error {
	if env == nil || maxBytes <= 0 {
		return nil
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if int64(len(encoded)) <= maxBytes {
		return nil
	}
	if len(env.FrozenImages) == 0 {
		return nil
	}
	withoutImages := *env
	withoutImages.FrozenImages = nil
	base, err := json.Marshal(withoutImages)
	if err != nil {
		return err
	}
	fairShare := int(max((maxBytes-int64(len(base))-inboxImageBudgetSafety)/int64(len(env.FrozenImages)), 1))
	for i, value := range env.FrozenImages {
		if compressed, changed := compressVisionDataURLToSize(value, fairShare); changed {
			env.FrozenImages[i] = compressed
		}
	}
	encoded, err = json.Marshal(env)
	if err != nil {
		return err
	}
	if int64(len(encoded)) <= maxBytes {
		return nil
	}
	return fmt.Errorf(
		"automatic image compression could not fit the queue item: %w",
		&sessioninbox.ItemTooLargeError{Size: int64(len(encoded)), Limit: maxBytes},
	)
}

func (c *Controller) RefreshInboxReferences(id string) error {
	st, err := c.ensureInbox()
	if err != nil {
		return err
	}
	_, env, err := st.ReadItem(id)
	if err != nil {
		return err
	}
	env.Refs = nil
	env.FrozenRefBlock, env.FrozenImages, env.ReferenceErrors = c.freezeInboxReferences(context.Background(), env.SubmitText, env.ExplicitRefs)
	if err := fitInboxEnvelopeImages(&env, sessioninbox.DefaultMaxItemBytes); err != nil {
		return err
	}
	_, err = st.UpdateItem(id, env)
	if err == nil && len(env.ReferenceErrors) > 0 {
		reason := strings.Join(env.ReferenceErrors, "; ")
		err = st.SetState(id, sessioninbox.StateBlocked, reason)
		_ = st.SetPaused(true)
	}
	return err
}

func applyInboxReferences(env sessioninbox.PromptEnvelope) (submit string, images []string, blockReason string, err error) {
	if len(env.ReferenceErrors) > 0 {
		return "", nil, strings.Join(env.ReferenceErrors, "; "), nil
	}
	submit = env.SubmitText
	images = append([]string(nil), env.FrozenImages...)
	if env.FrozenRefBlock != "" {
		submit = "Referenced context:\n\n" + env.FrozenRefBlock + "\n\n" + submit
		return submit, images, "", nil
	}
	if len(env.Refs) == 0 {
		return submit, images, "", nil
	}
	legacyBlock, bodies, materializeErr := sessioninbox.MaterializeRefs(context.Background(), "", env.Refs)
	if materializeErr != nil || legacyBlock != "" {
		return "", nil, legacyBlock, materializeErr
	}
	return sessioninbox.ApplyFrozenRefs(submit, bodies), images, "", nil
}
