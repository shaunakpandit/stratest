package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"stratest/internal/data"
)

type Data = data.Data

type Strategy struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tickers     []string `yaml:"tickers"`
	Timeframes  []string `yaml:"timeframes"`
	Base        string   `yaml:"base"`
	WarmupDays  int      `yaml:"warmup_days"`
	Model       string   `yaml:"model"`
	PromptHash  string   `yaml:"prompt_hash"`
	Code        string   `yaml:"code"`
}

var slug = regexp.MustCompile(`^[a-z0-9_-]+$`)

func CheckName(name string) error {
	if !slug.MatchString(name) {
		return fmt.Errorf("name %q must match [a-z0-9_-]+", name)
	}
	return nil
}

func Path(dir, name string) string { return filepath.Join(dir, name+".yaml") }

func Exists(dir, name string) bool {
	_, err := os.Stat(Path(dir, name))
	return err == nil
}

func Load(dir, name string) (*Strategy, error) {
	raw, err := os.ReadFile(Path(dir, name))
	if err != nil {
		return nil, fmt.Errorf("strategy %q not found in %s/ (see stratest list)", name, dir)
	}
	var s Strategy
	return &s, yaml.Unmarshal(raw, &s)
}

func LoadAll(dir string) ([]*Strategy, error) {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	var out []*Strategy
	for _, path := range paths {
		s, err := Load(dir, strings.TrimSuffix(filepath.Base(path), ".yaml"))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (s *Strategy) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// yaml.v3 only emits a readable `code: |` block when lines don't start with tabs.
	s.Code = strings.ReplaceAll(strings.TrimSpace(s.Code), "\t", "    ") + "\n"
	raw, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(Path(dir, s.Name), raw, 0o644)
}

func Hash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
