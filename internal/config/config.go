package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Runs before cmd's flag defaults are computed, so .env values like STRATEST_PROVIDER apply to them.
func init() { LoadEnv(".env") }

// LoadEnv reads KEY=value lines from path into the environment without overriding variables
// already set, so the binary works outside `nix develop` too.
func LoadEnv(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export ")), "=")
		if key = strings.TrimSpace(key); !ok || key == "" || strings.HasPrefix(key, "#") || os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, strings.Trim(strings.TrimSpace(value), `"'`))
	}
}

// Paths are relative to the working directory so a checkout is self-contained.
var (
	StrategiesDir = "strategies"
	CacheDir      = ".cache"
	PricesDir     = filepath.Join(CacheDir, "prices")
	LLMDir        = filepath.Join(CacheDir, "llm")
	ResultsDir    = filepath.Join(CacheDir, "results")
)

const (
	Capital       = 10_000.0
	FeeBps        = 1.0
	SlipBps       = 1.0
	Period        = "1y"
	Top           = 3
	FetchParallel = 4 // polite to Yahoo
	Benchmark     = "SPY"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func Model() string         { return env("STRATEST_MODEL", "anthropic/claude-sonnet-4.5") }
func Provider() string      { return env("STRATEST_PROVIDER", "yahoo") }
func OpenRouterKey() string { return os.Getenv("OPENROUTER_API_KEY") }
func PolygonKey() string    { return os.Getenv("POLYGON_API_KEY") }
