package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

func configCurrencyCommand(args []string) int {
	fs := flag.NewFlagSet("config currency", flag.ContinueOnError)
	local := fs.Bool("local", false, "unsupported; pricing currency is user-level only")
	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if *local {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "currency is user-level only; --local is not supported")
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 {
		configCurrencyUsage()
		return 2
	}
	if len(rest) == 0 {
		cfg, err := config.LoadForRootReadOnly(".")
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Printf("currency = %q (display: %s)\n", pricingCurrencyDisplay(cfg.DisplayCurrencyPref()), cfg.ResolveDisplayCurrency())
		return 0
	}
	mode, err := parseCLIPricingCurrency(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	path := config.UserConfigPath()
	if path == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "cannot resolve user config path")
		return 1
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg, err := config.LoadForEditReadOnlyStrict(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if err := cfg.SetDisplayCurrency(mode); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 2
	}
	resolved := cfg.ResolveDisplayCurrency()
	if err := cfg.SaveTo(path); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("currency = %q (display: %s, %s)\n", pricingCurrencyDisplay(mode), resolved, displayPath(path))
	return 0
}

func (m *chatTUI) runCurrencySubcommand(input string) tea.Cmd {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		cfg, err := config.Load()
		if err != nil {
			m.notice("currency: " + err.Error())
			return nil
		}
		m.notice(i18n.M.CurrencyHeader + "\n" + describePricingCurrencies(pricingCurrencyDisplay(cfg.DisplayCurrencyPref()), cfg.ResolveDisplayCurrency()) + "\n" + i18n.M.CurrencyHint)
		return nil
	}
	if len(args) > 2 {
		m.notice(i18n.M.CurrencyHint)
		return nil
	}
	mode, err := parseCLIPricingCurrency(args[1])
	if err != nil {
		m.notice(err.Error())
		return nil
	}
	if !m.runtimeSettingChangeReady() {
		return nil
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("currency: cannot resolve user config path")
		return nil
	}
	var resolved string
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit, err := config.LoadForEditReadOnlyStrict(path)
		if err != nil {
			return fmt.Errorf("load user config for currency edit: %w", err)
		}
		if err := edit.SetDisplayCurrency(mode); err != nil {
			return err
		}
		resolved = edit.ResolveDisplayCurrency()
		return edit.SaveTo(path)
	}(); err != nil {
		m.notice("currency: " + err.Error())
		return nil
	}

	success := fmt.Sprintf(i18n.M.CurrencyChangedFmt, pricingCurrencyDisplay(mode), resolved)
	if m.ctrl == nil {
		m.notice(success)
		return nil
	}
	return m.scheduleCurrentControllerRebuild("currency", success)
}

func parseCLIPricingCurrency(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "AUTO":
		return "", nil
	case "CNY":
		return "CNY", nil
	case "USD":
		return "USD", nil
	default:
		return "", fmt.Errorf("pricing currency %q: must be auto|CNY|USD", value)
	}
}

func pricingCurrencyDisplay(currency string) string {
	if strings.TrimSpace(currency) == "" {
		return "auto"
	}
	return strings.ToUpper(strings.TrimSpace(currency))
}

func describePricingCurrencies(current, resolved string) string {
	items := []string{"auto", "CNY", "USD"}
	var b strings.Builder
	for _, item := range items {
		marker := "  "
		if item == current {
			marker = "• "
		}
		hint := ""
		if item == "auto" {
			hint = " (" + resolved + ")"
		}
		fmt.Fprintf(&b, "%s%s%s\n", marker, item, hint)
	}
	return strings.TrimRight(b.String(), "\n")
}
