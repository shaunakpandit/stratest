package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"stratest/internal/config"
	"stratest/internal/data"
	"stratest/internal/report"
	"stratest/internal/strategy"
)

func init() {
	clear := &cobra.Command{Use: "clear", Short: "Clear caches (all of them unless flags pick some)", Args: cobra.NoArgs, RunE: clearCache}
	clear.Flags().Bool("prices", false, "clear downloaded prices")
	clear.Flags().Bool("llm", false, "clear generated code")
	clear.Flags().Bool("results", false, "clear backtest results")
	cache := &cobra.Command{Use: "cache", Short: "Manage caches"}
	cache.AddCommand(clear)
	root.AddCommand(
		&cobra.Command{Use: "list", Short: "List saved strategies", Args: cobra.NoArgs, RunE: list},
		&cobra.Command{Use: "show <name>", Short: "Print a strategy's config and code", Args: cobra.ExactArgs(1), RunE: show},
		&cobra.Command{Use: "regen <name>", Short: "Regenerate code from the saved description (overwrites)", Args: cobra.ExactArgs(1), RunE: regen},
		&cobra.Command{Use: "data", Short: "List downloaded price data", Args: cobra.NoArgs, RunE: listData},
		cache,
	)
}

func printTable(headers []string, rows [][]string) {
	fmt.Println(table.New().Border(lipgloss.RoundedBorder()).Headers(headers...).Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style { return lipgloss.NewStyle().Padding(0, 1).Bold(row == table.HeaderRow) }))
}

func list(*cobra.Command, []string) error {
	strategies, err := strategy.LoadAll(config.StrategiesDir)
	if err != nil {
		return err
	}
	rows := [][]string{}
	for _, s := range strategies {
		rows = append(rows, []string{s.Name, truncate(s.Description, 60), strings.Join(s.Timeframes, ", "), fmt.Sprint(len(s.Tickers))})
	}
	printTable([]string{"Name", "Description", "Timeframes", "Tickers"}, rows)
	return nil
}

func truncate(s string, n int) string {
	if runes := []rune(s); len(runes) > n {
		return string(runes[:n-1]) + "…"
	}
	return s
}

func show(_ *cobra.Command, args []string) error {
	s, err := strategy.Load(config.StrategiesDir, args[0])
	if err != nil {
		return err
	}
	fmt.Println(report.Panel([][2]string{
		{"Name", s.Name}, {"Description", s.Description}, {"Tickers", strings.Join(s.Tickers, ", ")},
		{"Timeframes", strings.Join(s.Timeframes, ", ")}, {"Base", s.Base}, {"Warm-up", fmt.Sprintf("%d days", s.WarmupDays)},
		{"Model", s.Model}, {"File", strategy.Path(config.StrategiesDir, s.Name)},
	}))
	fmt.Println(s.Code)
	return nil
}

func regen(_ *cobra.Command, args []string) error {
	s, err := strategy.Load(config.StrategiesDir, args[0])
	if err != nil {
		return err
	}
	model := config.Model()
	r, _, err := generate(s.Description, model, false)
	if err != nil {
		return err
	}
	fill(s, r, model)
	if err := s.Save(config.StrategiesDir); err != nil {
		return err
	}
	fmt.Println(describe(s, false))
	fmt.Printf("Saved. Run it with: stratest run %s\n", s.Name)
	return nil
}

func listData(*cobra.Command, []string) error {
	rows := [][]string{}
	for _, c := range data.ListCached(config.PricesDir) {
		rows = append(rows, []string{c.Ticker, c.Provider, c.Interval, c.From.Format("2006-01-02") + " → " + c.To.Format("2006-01-02"),
			fmt.Sprint(c.Bars), fmt.Sprintf("%.1f KB", float64(c.Size)/1024)})
	}
	printTable([]string{"Ticker", "Provider", "Interval", "Range", "Bars", "Size"}, rows)
	return nil
}

func clearCache(command *cobra.Command, _ []string) error {
	dirs := map[string]string{"prices": config.PricesDir, "llm": config.LLMDir, "results": config.ResultsDir}
	picked := []string{}
	for name := range dirs {
		if on, _ := command.Flags().GetBool(name); on {
			picked = append(picked, name)
		}
	}
	if len(picked) == 0 {
		picked = []string{"prices", "llm", "results"}
	}
	for _, name := range picked {
		if err := os.RemoveAll(filepath.Clean(dirs[name])); err != nil {
			return err
		}
		fmt.Println("cleared", dirs[name])
	}
	return nil
}
