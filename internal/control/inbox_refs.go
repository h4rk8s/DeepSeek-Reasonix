package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/attachment"
	"reasonix/internal/sessioninbox"
)

const inboxImageBudgetSafety = int64(4 << 10)

func (c *Controller) freezeInboxReferences(ctx context.Context, submit string, explicit []string) (string, []string, []string, []ImageReferenceFailure) {
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
		return "", nil, nil, nil
	}
	resolved := c.resolveUnscopedRefsForTurn(ctx, input)
	return resolved.block, resolved.images, resolved.errs, resolved.imageErrs
}

func (c *Controller) freezeInboxEnvelopeReferences(ctx context.Context, env *sessioninbox.PromptEnvelope, submit string, explicit []string, attachments ...SubmissionAttachment) error {
	var line strings.Builder
	line.WriteString(submit)
	for _, path := range explicit {
		path = strings.TrimSpace(path)
		if path != "" {
			line.WriteString(" @")
			line.WriteString(EscapeRefPath(path))
		}
	}
	frozen := map[string]*attachment.AttachmentRef{}
	for alias, digest := range env.ImageSourceRefs {
		for _, input := range env.ImageInputs {
			if input.Attachment != nil && input.Attachment.Content.Digest == digest {
				frozen[alias] = input.Attachment
				break
			}
		}
	}
	var inherited []attachment.Source
	if len(attachments) == 0 {
		for _, id := range env.AttachmentIdentities {
			key := "attachment:" + id
			if ref := frozen[key]; ref != nil {
				inherited = append(inherited, attachment.Source{Existing: ref, Path: key, DisplayName: ref.DisplayName})
			}
		}
	}
	prepared, failures := c.prepareSubmissionImagesContext(ctx, SubmissionRequest{Input: line.String(), Attachments: attachments, frozenSources: frozen, inheritedSources: inherited})
	if len(failures) > 0 {
		return ImageReferenceFailures(failures)
	}
	var imageErrs []ImageReferenceFailure
	env.FrozenRefBlock, env.FrozenImages, env.ReferenceErrors, imageErrs = c.freezeInboxReferences(contextWithPreparedImageReferences(ctx, prepared), submit, explicit)
	if len(imageErrs) > 0 {
		return ImageReferenceFailures(imageErrs)
	}
	if len(prepared.inputs) > 0 {
		env.ImageInputs = prepared.inputs
		env.ImageSourceRefs = prepared.byPath
		env.FrozenImages = nil
	} else if len(env.ImageInputs) > 0 {
		var sources []attachment.Source
		for _, input := range env.ImageInputs {
			if input.Attachment != nil {
				sources = append(sources, attachment.Source{Existing: input.Attachment, DisplayName: input.Attachment.DisplayName})
			}
		}
		if _, err := c.attachmentService().PrepareBatch(ctx, sources); err != nil {
			return ImageReferenceFailures(imageFailuresFromAttachment(err))
		}
	}
	return fitInboxEnvelopeImages(env, sessioninbox.DefaultMaxItemBytes)
}

// fitInboxEnvelopeImages keeps legacy inline image snapshots within the durable
// queue item limit. Current attachments are stored by digest outside the inbox
// envelope and therefore do not need lossy recompression here.
func fitInboxEnvelopeImages(env *sessioninbox.PromptEnvelope, maxBytes int64) error {
	if env == nil || maxBytes <= 0 {
		return nil
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if int64(len(encoded)) <= maxBytes || len(env.FrozenImages) == 0 {
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
	meta, env, err := st.ReadItem(id)
	if err != nil {
		return err
	}
	env.Refs = nil
	if err := c.freezeInboxEnvelopeReferences(context.Background(), &env, env.SubmitText, env.ExplicitRefs); err != nil {
		return err
	}
	_, err = st.UpdateItemIfVersion(id, env, sessioninbox.ContentVersion(meta))
	return err
}

func applyInboxReferences(env sessioninbox.PromptEnvelope) (submit string, images []string, blockReason string, err error) {
	if len(env.ReferenceErrors) > 0 {
		return "", nil, strings.Join(env.ReferenceErrors, "; "), nil
	}
	submit = env.SubmitText
	images = append([]string(nil), env.FrozenImages...)
	if len(env.ImageInputs) > 0 {
		images = nil
	}
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
