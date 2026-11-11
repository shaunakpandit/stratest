package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"stratest/internal/data"
)

var root = &cobra.Command{
	Use:           "stratest",
	Short:         "Describe a trading strategy in English, let an LLM write it, backtest it in the terminal",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// parseTickers uppercases, trims and dedupes a comma-separated list, keeping order. /NQ becomes
// Yahoo's continuous contract NQ=F, and "futures" expands to the most liquid contracts.
func parseTickers(lists ...string) []string {
	var out []string
	for _, list := range lists {
		for _, ticker := range strings.Split(list, ",") {
			expanded := []string{data.Symbol(strings.ToUpper(strings.TrimSpace(ticker)))}
			if expanded[0] == "FUTURES" {
				expanded = data.LiquidFutures
			}
			for _, ticker := range expanded {
				if ticker != "" && !slices.Contains(out, ticker) {
					out = append(out, ticker)
				}
			}
		}
	}
	return out
}
