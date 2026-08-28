package cli

import "testing"

func TestQueueSlashCompletionDescendsIntoSubcommands(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/qu")
	m.updateCompletion()

	if !m.completion.active || len(m.completion.items) == 0 || m.completion.items[0].label != "/queue" {
		t.Fatalf("typing /qu should lead with /queue: %+v", m.completion)
	}
	m.acceptCompletion()
	if got := m.input.Value(); got != "/queue " {
		t.Fatalf("accept /queue = %q, want argument prompt", got)
	}
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/queue should open its subcommand menu: %+v", m.completion)
	}
	for _, want := range []string{"list", "clear", "pause", "resume"} {
		if !hasLabel(m.completion.items, want) {
			t.Errorf("/queue menu missing %q: %v", want, labels(m.completion.items))
		}
	}
}
