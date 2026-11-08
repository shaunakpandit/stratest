package cmd

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/spf13/cobra"

	"stratest/internal/config"
	"stratest/internal/data"
	"stratest/internal/llm"
	"stratest/internal/report"
	"stratest/internal/strategy"
)

var newFlags struct {
	name, description, tickers string
	yes                        bool
}

func init() {
	command := &cobra.Command{Use: "new", Short: "Create a strategy with a guided wizard", Args: cobra.NoArgs, RunE: newStrategy}
	flags := command.Flags()
	flags.StringVar(&newFlags.name, "name", "", "strategy name (skips that step)")
	flags.StringVar(&newFlags.description, "desc", "", "plain-English description (skips that step)")
	flags.StringVar(&newFlags.tickers, "tickers", "", "comma-separated tickers (skips that step)")
	flags.BoolVar(&newFlags.yes, "yes", false, "skip review and confirmation; overwrite an existing strategy")
	root.AddCommand(command)
}

type draft struct {
	name, description string
	tickers           []string
}

func newStrategy(command *cobra.Command, _ []string) error {
	d := draft{name: newFlags.name, description: newFlags.description, tickers: parseTickers(newFlags.tickers)}
	if err := checkDraft(d); err != nil && newFlags.yes {
		return err
	}
	cached := cachedTickers()
	model := config.Model()
	askAll := false
	for {
		if err := askName(&d, askAll || strategy.CheckName(d.name) != nil); err != nil {
			return err
		}
		if askAll || newFlags.description == "" {
			if err := askDescription(&d); err != nil {
				return err
			}
		}
		if askAll || newFlags.tickers == "" {
			if err := askTickers(&d, cached); err != nil {
				return err
			}
		}
		if newFlags.yes {
			break
		}
		fmt.Println(report.Panel([][2]string{{"Name", d.name}, {"Description", d.description}, {"Tickers", tickerSummary(d.tickers, cached)}, {"Model", model}}))
		switch choose("Next", "Generate", "Edit", "Cancel") {
		case "Edit":
			askAll = true
			continue
		case "Cancel":
			return nil
		}
		break
	}

	useCache := true
	for {
		r, hit, err := generate(d.description, model, useCache)
		if err != nil {
			return err
		}
		s := &strategy.Strategy{Name: d.name, Description: d.description, Tickers: d.tickers}
		fill(s, r, model)
		if !newFlags.yes {
			fmt.Println(describe(s, hit))
			switch choose("Result", "Save", "Regenerate", "Edit description", "Discard") {
			case "Regenerate":
				useCache = false
				continue
			case "Edit description":
				if err := askDescription(&d); err != nil {
					return err
				}
				useCache = true
				continue
			case "Discard":
				return nil
			}
		}
		if err := s.Save(config.StrategiesDir); err != nil {
			return err
		}
		fmt.Fprintf(command.OutOrStdout(), "Saved. Run it with: stratest run %s\n", s.Name)
		return nil
	}
}

func checkDraft(d draft) error {
	if err := strategy.CheckName(d.name); err != nil {
		return err
	}
	if strings.TrimSpace(d.description) == "" {
		return fmt.Errorf("a description is required (--desc)")
	}
	if len(d.tickers) == 0 {
		return fmt.Errorf("at least one ticker is required (--tickers)")
	}
	return nil
}

func askName(d *draft, ask bool) error {
	for {
		if ask {
			err := huh.NewInput().Title("Strategy name").Description("lowercase letters, digits, _ and -").
				Value(&d.name).Validate(strategy.CheckName).Run()
			if err != nil {
				return err
			}
		}
		if newFlags.yes || !strategy.Exists(config.StrategiesDir, d.name) {
			return nil
		}
		overwrite := false
		if err := huh.NewConfirm().Title(fmt.Sprintf("%s already exists. Overwrite it?", d.name)).Value(&overwrite).Run(); err != nil {
			return err
		}
		if overwrite {
			return nil
		}
		ask = true
	}
}

func askDescription(d *draft) error {
	return huh.NewText().Title("Description").
		Placeholder("Buy when the 15m SMA(20) crosses above the 4h SMA(50), sell when it crosses back below.").
		Description("You can mention multiple timeframes, indicators, and entry or exit rules. Long-only unless you say to short.").
		Value(&d.description).Validate(func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("describe the strategy")
		}
		return nil
	}).Run()
}

func askTickers(d *draft, cached map[string]string) error {
	var selected, other []string
	for _, ticker := range d.tickers {
		if _, ok := cached[ticker]; ok {
			selected = append(selected, ticker)
		} else {
			other = append(other, ticker)
		}
	}
	extra := strings.Join(other, ", ")
	var fields []huh.Field
	if len(cached) > 0 {
		options := []huh.Option[string]{}
		for _, ticker := range slices.Sorted(maps.Keys(cached)) {
			options = append(options, huh.NewOption(fmt.Sprintf("%-6s (%s)", ticker, cached[ticker]), ticker))
		}
		fields = append(fields, huh.NewMultiSelect[string]().Title("Downloaded tickers").Options(options...).Value(&selected))
	}
	fields = append(fields, huh.NewInput().Title("Add other tickers (comma-separated, optional)").Value(&extra).
		Validate(func(s string) error {
			if len(parseTickers(strings.Join(selected, ","), s)) == 0 {
				return fmt.Errorf("at least one ticker is required")
			}
			return nil
		}))
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return err
	}
	d.tickers = parseTickers(strings.Join(selected, ","), extra)
	return nil
}

func choose(title string, options ...string) string {
	choice := options[0]
	if huh.NewSelect[string]().Title(title).Options(huh.NewOptions(options...)...).Value(&choice).Run() != nil {
		return options[len(options)-1] // aborting means cancel or discard
	}
	return choice
}

// cachedTickers maps each downloaded ticker to a label like "1h · 2024-06-01 → 2025-06-01 · yahoo".
func cachedTickers() map[string]string {
	out := map[string]string{}
	for _, c := range data.ListCached(config.PricesDir) {
		label := fmt.Sprintf("%s · %s → %s · %s", c.Interval, c.From.Format("2006-01-02"), c.To.Format("2006-01-02"), c.Provider)
		if out[c.Ticker] != "" {
			label = out[c.Ticker] + "; " + label
		}
		out[c.Ticker] = label
	}
	return out
}

func tickerSummary(tickers []string, cached map[string]string) string {
	hits := 0
	for _, ticker := range tickers {
		if _, ok := cached[ticker]; ok {
			hits++
		}
	}
	return fmt.Sprintf("%s  (%d cached, %d new)", strings.Join(tickers, ", "), hits, len(tickers)-hits)
}

func generate(description, model string, useCache bool) (r llm.Response, hit bool, err error) {
	action := func() { r, hit, err = llm.Generate(description, model, useCache) }
	if newFlags.yes {
		action()
		return
	}
	if spinnerErr := spinner.New().Title(" Generating strategy code with " + model + "…").Action(action).Run(); spinnerErr != nil {
		return r, hit, spinnerErr
	}
	return
}

func fill(s *strategy.Strategy, r llm.Response, model string) {
	s.Timeframes, s.Base, s.WarmupDays, s.Code = r.Timeframes, r.Base, r.WarmupDays, r.Code
	s.Model, s.PromptHash = model, llm.PromptHash(s.Description, model)
}

func describe(s *strategy.Strategy, hit bool) string {
	status := "generated, validation passed"
	if hit {
		status = "(cached)"
	}
	return report.Panel([][2]string{
		{"Status", status}, {"Timeframes", strings.Join(s.Timeframes, ", ")}, {"Base", s.Base},
		{"Warm-up", fmt.Sprintf("%d days", s.WarmupDays)}, {"Code", "\n" + strings.TrimSpace(s.Code)},
	})
}
