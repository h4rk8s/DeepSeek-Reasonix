package botruntime

import (
	"fmt"
	"strings"
	"time"

	"reasonix/internal/config"
)

func ForgetAutoSessionMappingsForPath(sessionPath string) error {
	target := normalizedBotSessionPath(sessionPath)
	if target == "" {
		return nil
	}
	userPath := config.UserConfigPath()
	if strings.TrimSpace(userPath) == "" {
		return nil
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()

	cfg, err := config.LoadForEditReadOnlyStrict(userPath)
	if err != nil {
		return fmt.Errorf("load user config to forget session mappings: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	changed := false
	for i := range cfg.Bot.Connections {
		conn := &cfg.Bot.Connections[i]
		next := conn.SessionMappings[:0]
		removed := false
		for _, mapping := range conn.SessionMappings {
			if strings.TrimSpace(mapping.SessionSource) == "auto" && normalizedBotSessionPath(mapping.SessionID) == target {
				removed = true
				continue
			}
			next = append(next, mapping)
		}
		if !removed {
			continue
		}
		conn.SessionMappings = next
		conn.UpdatedAt = now
		changed = true
	}
	// Legacy direct-channel auto mappings follow the same cleanup contract.
	ding := &cfg.Bot.Dingtalk
	dingNext := ding.SessionMappings[:0]
	dingRemoved := false
	for _, mapping := range ding.SessionMappings {
		if strings.TrimSpace(mapping.SessionSource) == "auto" && normalizedBotSessionPath(mapping.SessionID) == target {
			dingRemoved = true
			continue
		}
		dingNext = append(dingNext, mapping)
	}
	if dingRemoved {
		ding.SessionMappings = dingNext
		changed = true
	}
	if !changed {
		return nil
	}
	return cfg.SaveTo(userPath)
}
