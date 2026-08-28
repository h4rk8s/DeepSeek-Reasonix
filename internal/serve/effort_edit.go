package serve

import (
	"fmt"

	"reasonix/internal/config"
)

// saveEffortEdit owns only the locked load-modify-save cycle. Controller
// rebuilding happens after this returns and must never hold the config lock.
func saveEffortEdit(path string, entry *config.ProviderEntry, effort string) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	edit, err := config.LoadForEditReadOnlyStrict(path)
	if err != nil {
		return fmt.Errorf("load config for effort edit: %w", err)
	}
	if err := applyEffortEdit(edit, entry, effort); err != nil {
		return err
	}
	if err := edit.SaveTo(path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
