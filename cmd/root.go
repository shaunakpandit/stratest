package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
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

// parseTickers uppercases, trims and dedupes a comma-separated list, keeping order.
func parseTickers(lists ...string) []string {
	var out []string
	for _, list := range lists {
		for _, ticker := range strings.Split(list, ",") {
			if ticker = strings.ToUpper(strings.TrimSpace(ticker)); ticker != "" && !slices.Contains(out, ticker) {
				out = append(out, ticker)
			}
		}
	}
	return out
}
