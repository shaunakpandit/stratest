package config

import (
	"os"
	"path/filepath"
)

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
