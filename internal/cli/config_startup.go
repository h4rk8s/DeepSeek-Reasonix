package cli

import (
	"fmt"
	"os"
	"reasonix/internal/config"
)

func migrateLegacyConfigForCLI() error {
	if _, err := config.MigrateLegacyIfNeeded(); err != nil {
		return fmt.Errorf("refusing to run with invalid config: config migration failed: %w", err)
	}
	if changed, err := config.ApplyUserConfigUpgradesOnStartup(config.UserConfigPath()); err != nil {
		return fmt.Errorf("refusing to run with invalid config: config upgrade failed: %w", err)
	} else if changed {
		if cfg, err := config.LoadUserConfigReadOnly(); err == nil {
			if summary := cfg.OpenCodeGoUpgradeSummary(); summary != "" {
				fmt.Fprintln(os.Stderr, summary)
			}
		}
	}
	return nil
}
