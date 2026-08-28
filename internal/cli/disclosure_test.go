package cli

import "testing"

func TestDisclosureModelOwnsReasoningAndImageEntries(t *testing.T) {
	d := newDisclosureModel()
	d.remember(1, "* Thought for 1s", "reasoning", false, transcriptDisclosureReasoning, 3)
	d.remember(2, "◩ Image understood", "image detail", false, transcriptDisclosureImageUnderstanding, 3)

	reasoning, ok := d.entryAt(1)
	if !ok || reasoning.kind != transcriptDisclosureReasoning || reasoning.raw != "reasoning" {
		t.Fatalf("reasoning disclosure = %+v, ok=%v", reasoning, ok)
	}
	image, ok := d.entryAt(2)
	if !ok || image.kind != transcriptDisclosureImageUnderstanding || image.raw != "image detail" {
		t.Fatalf("image disclosure = %+v, ok=%v", image, ok)
	}
	if len(d.entries) != 2 || d.nextID != 2 {
		t.Fatalf("stable disclosure identity lost: entries=%d nextID=%d", len(d.entries), d.nextID)
	}

	d.shift(1, 2, 5)
	if got, ok := d.entryAt(3); !ok || got != reasoning {
		t.Fatalf("reasoning disclosure did not follow transcript shift: got=%+v ok=%v", got, ok)
	}
	if got, ok := d.entryAt(4); !ok || got != image {
		t.Fatalf("image disclosure did not follow transcript shift: got=%+v ok=%v", got, ok)
	}

	d.truncate(4)
	if _, ok := d.entryAt(4); ok {
		t.Fatal("truncated image disclosure remained indexed")
	}
	if got, ok := d.entryAt(3); !ok || got != reasoning {
		t.Fatalf("untruncated reasoning disclosure was lost: got=%+v ok=%v", got, ok)
	}
}
