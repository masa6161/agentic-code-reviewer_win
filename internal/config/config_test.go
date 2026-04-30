package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadFromDirWithWarnings_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "test pattern"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromDirWithWarnings(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Config.Filters.ExcludePatterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(result.Config.Filters.ExcludePatterns))
	}
	if result.Config.Filters.ExcludePatterns[0] != "test pattern" {
		t.Errorf("expected 'test pattern', got %q", result.Config.Filters.ExcludePatterns[0])
	}
}

func TestLoadFromDirWithWarnings_NoConfig(t *testing.T) {
	dir := t.TempDir()

	result, err := LoadFromDirWithWarnings(dir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
	}
}

func TestLoadFromPathWithWarnings_FileNotFound(t *testing.T) {
	result, err := LoadFromPathWithWarnings("/nonexistent/path/.acr.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if result.Config == nil {
		t.Fatal("expected non-nil config")
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings for missing file, got: %v", result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "Next\\.js forbids"
    - "deprecated API"
    - "consider using"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"Next\\.js forbids", "deprecated API", "consider using"}
	if len(result.Config.Filters.ExcludePatterns) != len(expected) {
		t.Fatalf("expected %d patterns, got %d", len(expected), len(result.Config.Filters.ExcludePatterns))
	}
	for i, pattern := range expected {
		if result.Config.Filters.ExcludePatterns[i] != pattern {
			t.Errorf("pattern %d: expected %q, got %q", i, pattern, result.Config.Filters.ExcludePatterns[i])
		}
	}
}

func TestLoadFromPathWithWarnings_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
	}
}

func TestLoadFromPathWithWarnings_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "valid"
    invalid yaml here
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFromPathWithWarnings_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "valid pattern"
    - "[invalid regex"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestLoadFromPathWithWarnings_EmptyPatterns(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns: []
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
	}
}

func TestMerge_NilConfig(t *testing.T) {
	cliPatterns := []string{"cli-pattern"}
	result := Merge(nil, cliPatterns)

	if len(result) != 1 || result[0] != "cli-pattern" {
		t.Errorf("expected cli patterns only, got: %v", result)
	}
}

func TestMerge_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	cliPatterns := []string{"cli-pattern"}
	result := Merge(cfg, cliPatterns)

	if len(result) != 1 || result[0] != "cli-pattern" {
		t.Errorf("expected cli patterns only, got: %v", result)
	}
}

func TestMerge_ConfigOnly(t *testing.T) {
	cfg := &Config{
		Filters: FilterConfig{
			ExcludePatterns: []string{"config-pattern-1", "config-pattern-2"},
		},
	}
	result := Merge(cfg, nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(result))
	}
	if result[0] != "config-pattern-1" || result[1] != "config-pattern-2" {
		t.Errorf("unexpected patterns: %v", result)
	}
}

func TestMerge_BothConfigAndCLI(t *testing.T) {
	cfg := &Config{
		Filters: FilterConfig{
			ExcludePatterns: []string{"config-pattern"},
		},
	}
	cliPatterns := []string{"cli-pattern"}
	result := Merge(cfg, cliPatterns)

	if len(result) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(result))
	}
	// Config patterns come first, then CLI patterns
	if result[0] != "config-pattern" {
		t.Errorf("expected config pattern first, got: %s", result[0])
	}
	if result[1] != "cli-pattern" {
		t.Errorf("expected cli pattern second, got: %s", result[1])
	}
}

func TestMerge_BothEmpty(t *testing.T) {
	cfg := &Config{}
	result := Merge(cfg, nil)

	if len(result) != 0 {
		t.Errorf("expected empty result, got: %v", result)
	}
}

// Tests for expanded config schema

func TestLoadFromPathWithWarnings_FullConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 10
concurrency: 5
base: develop
timeout: 10m
retries: 3
filters:
  exclude_patterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := result.Config

	if cfg.Reviewers == nil || *cfg.Reviewers != 10 {
		t.Errorf("expected reviewers=10, got %v", cfg.Reviewers)
	}
	if cfg.Concurrency == nil || *cfg.Concurrency != 5 {
		t.Errorf("expected concurrency=5, got %v", cfg.Concurrency)
	}
	if cfg.Base == nil || *cfg.Base != "develop" {
		t.Errorf("expected base=develop, got %v", cfg.Base)
	}
	if cfg.Timeout == nil || cfg.Timeout.AsDuration() != 10*time.Minute {
		t.Errorf("expected timeout=10m, got %v", cfg.Timeout)
	}
	if cfg.Retries == nil || *cfg.Retries != 3 {
		t.Errorf("expected retries=3, got %v", cfg.Retries)
	}
	if len(cfg.Filters.ExcludePatterns) != 1 || cfg.Filters.ExcludePatterns[0] != "test" {
		t.Errorf("expected exclude_patterns=[test], got %v", cfg.Filters.ExcludePatterns)
	}
}

func TestLoadFromPathWithWarnings_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 3
base: feature-branch
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := result.Config

	if cfg.Reviewers == nil || *cfg.Reviewers != 3 {
		t.Errorf("expected reviewers=3, got %v", cfg.Reviewers)
	}
	if cfg.Concurrency != nil {
		t.Errorf("expected concurrency=nil, got %v", cfg.Concurrency)
	}
	if cfg.Base == nil || *cfg.Base != "feature-branch" {
		t.Errorf("expected base=feature-branch, got %v", cfg.Base)
	}
	if cfg.Timeout != nil {
		t.Errorf("expected timeout=nil, got %v", cfg.Timeout)
	}
	if cfg.Retries != nil {
		t.Errorf("expected retries=nil, got %v", cfg.Retries)
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected time.Duration
		wantErr  bool
	}{
		{"duration string 5m", "timeout: 5m", 5 * time.Minute, false},
		{"duration string 300s", "timeout: 300s", 5 * time.Minute, false},
		{"duration string 1h30m", "timeout: 1h30m", 90 * time.Minute, false},
		{"integer seconds", "timeout: 300", 5 * time.Minute, false},
		{"invalid string", "timeout: invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg struct {
				Timeout *Duration `yaml:"timeout"`
			}
			err := yaml.Unmarshal([]byte(tt.yaml), &cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Timeout == nil {
				t.Fatal("expected timeout to be set")
			}
			if cfg.Timeout.AsDuration() != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, cfg.Timeout.AsDuration())
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid config", Config{Reviewers: ptr(5), Retries: ptr(2)}, false},
		{"reviewers zero", Config{Reviewers: ptr(0)}, true},
		{"reviewers negative", Config{Reviewers: ptr(-1)}, true},
		{"concurrency negative", Config{Concurrency: ptr(-1)}, true},
		{"concurrency zero valid", Config{Concurrency: ptr(0)}, false},
		{"retries negative", Config{Retries: ptr(-1)}, true},
		{"retries zero valid", Config{Retries: ptr(0)}, false},
		{"timeout negative", Config{Timeout: durationPtr(-time.Second)}, true},
		{"timeout zero", Config{Timeout: durationPtr(0)}, true},
		{"all nil valid", Config{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolve_FlagOverridesAll(t *testing.T) {
	cfg := &Config{Reviewers: ptr(3)}
	envState := EnvState{Reviewers: 5, ReviewersSet: true}
	flagState := FlagState{ReviewersSet: true}
	flagValues := ResolvedConfig{Reviewers: 10}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 10 {
		t.Errorf("expected flag value 10, got %d", result.Reviewers)
	}
}

func TestResolve_EnvOverridesConfig(t *testing.T) {
	cfg := &Config{Reviewers: ptr(3)}
	envState := EnvState{Reviewers: 5, ReviewersSet: true}
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 5 {
		t.Errorf("expected env value 5, got %d", result.Reviewers)
	}
}

func TestResolve_ConfigOverridesDefault(t *testing.T) {
	cfg := &Config{Reviewers: ptr(3)}
	envState := EnvState{}   // no env vars set
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 3 {
		t.Errorf("expected config value 3, got %d", result.Reviewers)
	}
}

func TestResolve_DefaultsUsedWhenNothingSet(t *testing.T) {
	cfg := &Config{} // empty config
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != Defaults.Reviewers {
		t.Errorf("expected default reviewers %d, got %d", Defaults.Reviewers, result.Reviewers)
	}
	if result.Base != Defaults.Base {
		t.Errorf("expected default base %q, got %q", Defaults.Base, result.Base)
	}
	if result.Timeout != Defaults.Timeout {
		t.Errorf("expected default timeout %v, got %v", Defaults.Timeout, result.Timeout)
	}
	if result.Retries != Defaults.Retries {
		t.Errorf("expected default retries %d, got %d", Defaults.Retries, result.Retries)
	}
}

func TestResolve_NilConfig(t *testing.T) {
	result := Resolve(nil, EnvState{}, FlagState{}, ResolvedConfig{})

	if result.Reviewers != Defaults.Reviewers {
		t.Errorf("expected default reviewers %d, got %d", Defaults.Reviewers, result.Reviewers)
	}
}

func TestResolve_MixedSources(t *testing.T) {
	// reviewers from config, base from env, timeout from flag
	cfg := &Config{
		Reviewers: ptr(3),
		Base:      strPtr("config-base"),
		Timeout:   durationPtr(1 * time.Minute),
	}
	envState := EnvState{
		Base:    "env-base",
		BaseSet: true,
	}
	flagState := FlagState{
		TimeoutSet: true,
	}
	flagValues := ResolvedConfig{
		Timeout: 10 * time.Minute,
	}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 3 {
		t.Errorf("expected config reviewers 3, got %d", result.Reviewers)
	}
	if result.Base != "env-base" {
		t.Errorf("expected env base 'env-base', got %q", result.Base)
	}
	if result.Timeout != 10*time.Minute {
		t.Errorf("expected flag timeout 10m, got %v", result.Timeout)
	}
}

func TestResolve_PhaseTimeoutPrecedence(t *testing.T) {
	cfg := &Config{
		SummarizerTimeout: durationPtr(3 * time.Minute),
		FPFilterTimeout:   durationPtr(4 * time.Minute),
	}
	envState := EnvState{
		SummarizerTimeout:    6 * time.Minute,
		SummarizerTimeoutSet: true,
		FPFilterTimeout:      7 * time.Minute,
		FPFilterTimeoutSet:   true,
	}
	flagState := FlagState{
		SummarizerTimeoutSet: true,
	}
	flagValues := ResolvedConfig{
		SummarizerTimeout: 8 * time.Minute,
	}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.SummarizerTimeout != 8*time.Minute {
		t.Errorf("expected flag summarizer timeout 8m, got %v", result.SummarizerTimeout)
	}
	if result.FPFilterTimeout != 7*time.Minute {
		t.Errorf("expected env fp_filter timeout 7m, got %v", result.FPFilterTimeout)
	}
}

func TestLoadFromPathWithWarnings_InvalidReviewers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 0
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for reviewers=0")
	}
}

func TestLoadFromPathWithWarnings_InvalidTimeout(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `timeout: -5m
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestLoadFromPathWithWarnings_PreservesWarningsOnValidationError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	// Config with both an unknown key (produces warning) and invalid value (produces error)
	content := `reviewers: 0
unknown_field: true
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for reviewers=0")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on validation error")
	}
	if result.Config == nil {
		t.Fatal("expected non-nil Config in result")
	}
	if result.Config.Reviewers == nil || *result.Config.Reviewers != 0 {
		t.Error("expected parsed Config to contain reviewers=0")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected unknown-key warning to be preserved alongside validation error")
	}
}

// Tests for unknown key warnings

func TestLoadFromPathWithWarnings_UnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 5
unknownkey: value
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	if result.Warnings[0] != `unknown key "unknownkey" in .acr.yaml` {
		t.Errorf("unexpected warning: %s", result.Warnings[0])
	}
	// Config should still be parsed
	if result.Config.Reviewers == nil || *result.Config.Reviewers != 5 {
		t.Errorf("expected reviewers=5, got %v", result.Config.Reviewers)
	}
}

func TestLoadFromPathWithWarnings_UnknownKeyWithSuggestion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filtrs:
  exclude_patterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	expected := `unknown key "filtrs" in .acr.yaml (did you mean "filters"?)`
	if result.Warnings[0] != expected {
		t.Errorf("expected warning %q, got %q", expected, result.Warnings[0])
	}
}

func TestLoadFromPathWithWarnings_UnknownFilterKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_paterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	expected := `unknown key "exclude_paterns" in filters section of .acr.yaml (did you mean "exclude_patterns"?)`
	if result.Warnings[0] != expected {
		t.Errorf("expected warning %q, got %q", expected, result.Warnings[0])
	}
}

func TestLoadFromPathWithWarnings_MultipleUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewrs: 5
tiemout: 10m
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_NoWarningsForValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 5
concurrency: 3
base: main
timeout: 5m
retries: 2
summarizer_timeout: 6m
fp_filter_timeout: 7m
guidance_file: guidance.md
filters:
  exclude_patterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_NoWarningsForEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "abcd", 1},
		{"filters", "filtrs", 1},
		{"exclude_patterns", "exclude_paterns", 1},
		{"reviewers", "reviewrs", 1},
		{"timeout", "tiemout", 2},
		{"totally_different", "abc", 16},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			got := levenshtein(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, expected %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestFindSimilar(t *testing.T) {
	candidates := []string{"reviewers", "concurrency", "base", "timeout", "retries", "filters"}

	tests := []struct {
		input    string
		expected string
	}{
		{"reviewrs", "reviewers"},
		{"filtrs", "filters"},
		{"tiemout", "timeout"},
		{"totally_unrelated_name", ""},
		{"reviewers", "reviewers"}, // exact match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := findSimilar(tt.input, candidates)
			if got != tt.expected {
				t.Errorf("findSimilar(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

// Helper functions
func ptr(i int) *int { return &i }

func strPtr(s string) *string { return &s }

func durationPtr(d time.Duration) *Duration {
	dur := Duration(d)
	return &dur
}

func TestResolveGuidance(t *testing.T) {
	// Create temp files for guidance file tests
	dir := t.TempDir()
	flagGuidanceFile := filepath.Join(dir, "flag_guidance.txt")
	envGuidanceFile := filepath.Join(dir, "env_guidance.txt")
	configGuidanceFile := filepath.Join(dir, "config_guidance.txt")

	if err := os.WriteFile(flagGuidanceFile, []byte("guidance from flag file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envGuidanceFile, []byte("guidance from env file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configGuidanceFile, []byte("guidance from config file"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		cfg        *Config
		envState   EnvState
		flagState  FlagState
		flagValues ResolvedConfig
		want       string
		wantErr    bool
	}{
		{
			name: "flag guidance text wins over all",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "env guidance",
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			flagState: FlagState{
				GuidanceSet:     true,
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				Guidance:     "flag guidance",
				GuidanceFile: flagGuidanceFile,
			},
			want: "flag guidance",
		},
		{
			name: "flag guidance-file wins over env/config",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "env guidance",
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			flagState: FlagState{
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				GuidanceFile: flagGuidanceFile,
			},
			want: "guidance from flag file",
		},
		{
			name: "env ACR_GUIDANCE wins over config",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "env guidance",
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			want: "env guidance",
		},
		{
			name: "env ACR_GUIDANCE_FILE wins over config",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			want: "guidance from env file",
		},
		{
			name: "config guidance_file works",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			want: "guidance from config file",
		},
		{
			name: "nothing set returns empty",
			want: "",
		},
		{
			name: "empty strings result in empty guidance",
			cfg: &Config{
				GuidanceFile: strPtr(""),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "",
				GuidanceFileSet: true,
				GuidanceFile:    "",
			},
			flagState: FlagState{
				GuidanceSet:     true,
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				Guidance:     "",
				GuidanceFile: "",
			},
			want: "",
		},
		{
			name: "error reading flag guidance file",
			flagState: FlagState{
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				GuidanceFile: "/nonexistent/guidance.txt",
			},
			wantErr: true,
		},
		{
			name: "error reading env guidance file",
			envState: EnvState{
				GuidanceFileSet: true,
				GuidanceFile:    "/nonexistent/guidance.txt",
			},
			wantErr: true,
		},
		{
			name: "error reading config guidance file",
			cfg: &Config{
				GuidanceFile: strPtr("/nonexistent/guidance.txt"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveGuidance(tt.cfg, tt.envState, tt.flagState, tt.flagValues, "")
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveGuidance() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got != tt.want {
					t.Errorf("ResolveGuidance() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// Tests for agent config

func TestLoadFromPathWithWarnings_ReviewerAgentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agent: claude
reviewers: 5
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := result.Config

	if cfg.ReviewerAgent == nil || *cfg.ReviewerAgent != "claude" {
		t.Errorf("expected reviewer_agent=claude, got %v", cfg.ReviewerAgent)
	}
}

func TestValidate_ReviewerAgent(t *testing.T) {
	tests := []struct {
		name    string
		agent   string
		wantErr bool
	}{
		{"valid codex", "codex", false},
		{"valid claude", "claude", false},
		{"valid gemini", "gemini", false},
		{"invalid agent", "invalid", true},
		{"empty agent", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ReviewerAgent: strPtr(tt.agent)}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolve_ReviewerAgents_FlagOverridesAll(t *testing.T) {
	cfg := &Config{ReviewerAgent: strPtr("gemini")}
	envState := EnvState{ReviewerAgents: []string{"claude"}, ReviewerAgentsSet: true}
	flagState := FlagState{ReviewerAgentsSet: true}
	flagValues := ResolvedConfig{ReviewerAgents: []string{"codex"}}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "codex" {
		t.Errorf("expected flag value ['codex'], got %v", result.ReviewerAgents)
	}
}

func TestResolve_ReviewerAgents_EnvOverridesConfig(t *testing.T) {
	cfg := &Config{ReviewerAgent: strPtr("gemini")}
	envState := EnvState{ReviewerAgents: []string{"claude"}, ReviewerAgentsSet: true}
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "claude" {
		t.Errorf("expected env value ['claude'], got %v", result.ReviewerAgents)
	}
}

func TestResolve_ReviewerAgents_ConfigOverridesDefault(t *testing.T) {
	cfg := &Config{ReviewerAgent: strPtr("gemini")}
	envState := EnvState{}   // no env vars set
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "gemini" {
		t.Errorf("expected config value ['gemini'], got %v", result.ReviewerAgents)
	}
}

func TestResolve_ReviewerAgents_DefaultsToCodex(t *testing.T) {
	cfg := &Config{} // empty config
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "codex" {
		t.Errorf("expected default reviewer_agents ['codex'], got %v", result.ReviewerAgents)
	}
}

func TestLoadEnvState_ReviewerAgents(t *testing.T) {
	// Save and restore original env
	original := os.Getenv("ACR_REVIEWER_AGENT")
	defer func() {
		if original != "" {
			os.Setenv("ACR_REVIEWER_AGENT", original)
		} else {
			os.Unsetenv("ACR_REVIEWER_AGENT")
		}
	}()

	os.Setenv("ACR_REVIEWER_AGENT", "claude")
	state, warnings := LoadEnvState()

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !state.ReviewerAgentsSet {
		t.Error("expected ReviewerAgentsSet to be true")
	}
	if len(state.ReviewerAgents) != 1 || state.ReviewerAgents[0] != "claude" {
		t.Errorf("expected reviewer_agents=['claude'], got %v", state.ReviewerAgents)
	}
}

func TestLoadEnvState_ReviewerAgents_NotSet(t *testing.T) {
	// Save and restore original env
	original := os.Getenv("ACR_REVIEWER_AGENT")
	defer func() {
		if original != "" {
			os.Setenv("ACR_REVIEWER_AGENT", original)
		} else {
			os.Unsetenv("ACR_REVIEWER_AGENT")
		}
	}()

	os.Unsetenv("ACR_REVIEWER_AGENT")
	state, warnings := LoadEnvState()

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if state.ReviewerAgentsSet {
		t.Error("expected ReviewerAgentsSet to be false")
	}
	if len(state.ReviewerAgents) != 0 {
		t.Errorf("expected empty reviewer_agents, got %v", state.ReviewerAgents)
	}
}

func TestResolveGuidance_Precedence(t *testing.T) {
	// Test that verifies the exact precedence order
	dir := t.TempDir()
	guidanceFile := filepath.Join(dir, "guidance.txt")
	if err := os.WriteFile(guidanceFile, []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	// All sources set, flag guidance text should win
	cfg := &Config{
		GuidanceFile: strPtr(guidanceFile),
	}
	envState := EnvState{
		GuidanceSet:     true,
		Guidance:        "env guidance",
		GuidanceFileSet: true,
		GuidanceFile:    guidanceFile,
	}
	flagState := FlagState{
		GuidanceSet:     true,
		GuidanceFileSet: false,
	}
	flagValues := ResolvedConfig{
		Guidance:     "flag guidance",
		GuidanceFile: "",
	}

	got, err := ResolveGuidance(cfg, envState, flagState, flagValues, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "flag guidance" {
		t.Errorf("expected 'flag guidance', got %q", got)
	}
}

func TestResolveGuidance_ConfigFileRelativePath(t *testing.T) {
	// Create a temp directory structure:
	// tempdir/
	//   guidance/
	//     review.md
	dir := t.TempDir()
	guidanceDir := filepath.Join(dir, "guidance")
	if err := os.MkdirAll(guidanceDir, 0755); err != nil {
		t.Fatalf("failed to create guidance dir: %v", err)
	}
	guidanceFile := filepath.Join(guidanceDir, "review.md")
	guidanceContent := "custom review guidance from file"
	if err := os.WriteFile(guidanceFile, []byte(guidanceContent), 0644); err != nil {
		t.Fatalf("failed to write guidance file: %v", err)
	}

	// Config with relative path
	relativePath := "guidance/review.md"
	cfg := &Config{
		GuidanceFile: &relativePath,
	}
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	// Resolve with configDir set to temp directory
	got, err := ResolveGuidance(cfg, envState, flagState, flagValues, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != guidanceContent {
		t.Errorf("ResolveGuidance() = %q, want %q", got, guidanceContent)
	}
}

func TestResolveGuidance_ConfigFileAbsolutePath(t *testing.T) {
	// Create a temp file with guidance content
	dir := t.TempDir()
	guidanceFile := filepath.Join(dir, "guidance.md")
	guidanceContent := "absolute path guidance"
	if err := os.WriteFile(guidanceFile, []byte(guidanceContent), 0644); err != nil {
		t.Fatalf("failed to write guidance file: %v", err)
	}

	// Config with absolute path - should work regardless of configDir
	cfg := &Config{
		GuidanceFile: &guidanceFile,
	}
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	// Resolve with a different configDir - absolute path should still work
	got, err := ResolveGuidance(cfg, envState, flagState, flagValues, "/some/other/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != guidanceContent {
		t.Errorf("ResolveGuidance() = %q, want %q", got, guidanceContent)
	}
}

// Tests for malformed environment variable warnings

// clearACREnv unsets all ACR_* env vars to isolate tests from ambient environment.
// Uses t.Setenv("VAR", "") then os.Unsetenv to get automatic restore on test cleanup.
func clearACREnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ACR_REVIEWERS", "ACR_LARGE_DIFF_REVIEWERS", "ACR_MEDIUM_DIFF_REVIEWERS", "ACR_SMALL_DIFF_REVIEWERS",
		"ACR_CONCURRENCY", "ACR_BASE_REF", "ACR_TIMEOUT",
		"ACR_RETRIES", "ACR_FETCH", "ACR_REVIEWER_AGENT", "ACR_SUMMARIZER_AGENT",
		"ACR_ARCH_REVIEWER_AGENT", "ACR_DIFF_REVIEWER_AGENTS",
		"ACR_SUMMARIZER_TIMEOUT", "ACR_FP_FILTER_TIMEOUT",
		"ACR_GUIDANCE", "ACR_GUIDANCE_FILE", "ACR_FP_FILTER", "ACR_FP_THRESHOLD",
		"ACR_PR_FEEDBACK", "ACR_PR_FEEDBACK_AGENT",
	} {
		t.Setenv(key, os.Getenv(key)) // register for restore
		os.Unsetenv(key)
	}
}

// hasWarningContaining checks if any warning contains the given substring.
func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestLoadEnvState_MalformedReviewers(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_REVIEWERS", "abc")
	state, warnings := LoadEnvState()
	if state.ReviewersSet {
		t.Error("expected ReviewersSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_REVIEWERS") {
		t.Errorf("expected warning about ACR_REVIEWERS, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedConcurrency(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_CONCURRENCY", "xyz")
	state, warnings := LoadEnvState()
	if state.ConcurrencySet {
		t.Error("expected ConcurrencySet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_CONCURRENCY") {
		t.Errorf("expected warning about ACR_CONCURRENCY, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedTimeout(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_TIMEOUT", "notaduration")
	state, warnings := LoadEnvState()
	if state.TimeoutSet {
		t.Error("expected TimeoutSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_TIMEOUT") {
		t.Errorf("expected warning about ACR_TIMEOUT, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedRetries(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_RETRIES", "nope")
	state, warnings := LoadEnvState()
	if state.RetriesSet {
		t.Error("expected RetriesSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_RETRIES") {
		t.Errorf("expected warning about ACR_RETRIES, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFetch(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FETCH", "maybe")
	state, warnings := LoadEnvState()
	if state.FetchSet {
		t.Error("expected FetchSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_FETCH") {
		t.Errorf("expected warning about ACR_FETCH, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFPFilter(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FP_FILTER", "maybe")
	state, warnings := LoadEnvState()
	if state.FPFilterSet {
		t.Error("expected FPFilterSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_FP_FILTER") {
		t.Errorf("expected warning about ACR_FP_FILTER, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedPRFeedback(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_PR_FEEDBACK", "maybe")
	state, warnings := LoadEnvState()
	if state.PRFeedbackEnabledSet {
		t.Error("expected PRFeedbackEnabledSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_PR_FEEDBACK") {
		t.Errorf("expected warning about ACR_PR_FEEDBACK, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFPThreshold_NotInt(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FP_THRESHOLD", "abc")
	state, warnings := LoadEnvState()
	if state.FPThresholdSet {
		t.Error("expected FPThresholdSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_FP_THRESHOLD") {
		t.Errorf("expected warning about ACR_FP_THRESHOLD, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFPThreshold_OutOfRange(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FP_THRESHOLD", "200")
	state, warnings := LoadEnvState()
	if state.FPThresholdSet {
		t.Error("expected FPThresholdSet to be false for out-of-range value")
	}
	if !hasWarningContaining(warnings, "out of range") {
		t.Errorf("expected out-of-range warning, got %v", warnings)
	}
}

func TestLoadEnvState_NoWarningsForValidValues(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_REVIEWERS", "5")
	t.Setenv("ACR_TIMEOUT", "10m")
	t.Setenv("ACR_SUMMARIZER_TIMEOUT", "6m")
	t.Setenv("ACR_FP_FILTER_TIMEOUT", "7m")
	t.Setenv("ACR_FETCH", "true")
	_, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for valid values, got %v", warnings)
	}
}

func TestLoadEnvState_PhaseTimeouts(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_SUMMARIZER_TIMEOUT", "360") // integer seconds
	t.Setenv("ACR_FP_FILTER_TIMEOUT", "7m")

	state, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if !state.SummarizerTimeoutSet || state.SummarizerTimeout != 6*time.Minute {
		t.Fatalf("expected summarizer timeout 360s/6m set=true, got %v set=%v", state.SummarizerTimeout, state.SummarizerTimeoutSet)
	}
	if !state.FPFilterTimeoutSet || state.FPFilterTimeout != 7*time.Minute {
		t.Fatalf("expected fp filter timeout 7m set=true, got %v set=%v", state.FPFilterTimeout, state.FPFilterTimeoutSet)
	}
}

func TestLoadEnvState_InvalidPhaseTimeouts(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_SUMMARIZER_TIMEOUT", "bad")
	t.Setenv("ACR_FP_FILTER_TIMEOUT", "still-bad")

	_, warnings := LoadEnvState()
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
}

// Tests for deprecated reviewer_agent config key

func TestLoadFromPathWithWarnings_DeprecatedReviewerAgent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agent: claude
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "deprecated") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected deprecation warning, got warnings: %v", result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_DeprecatedReviewerAgentWithReviewerAgents(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agent: claude
reviewer_agents:
  - codex
  - gemini
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasDeprecated := false
	hasPrecedence := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "deprecated") {
			hasDeprecated = true
		}
		if strings.Contains(w, "takes precedence") {
			hasPrecedence = true
		}
	}
	if !hasDeprecated {
		t.Errorf("expected deprecation warning, got: %v", result.Warnings)
	}
	if !hasPrecedence {
		t.Errorf("expected precedence warning, got: %v", result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_NoDeprecationWithoutReviewerAgent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agents:
  - codex
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range result.Warnings {
		if strings.Contains(w, "deprecated") {
			t.Errorf("unexpected deprecation warning: %s", w)
		}
	}
}

func TestResolvedConfig_Validate_Valid(t *testing.T) {
	cfg := Defaults
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected defaults to be valid, got: %v", err)
	}
}

func TestResolvedConfig_Validate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*ResolvedConfig)
		wantMsg string
	}{
		{
			name:    "reviewers too low",
			modify:  func(c *ResolvedConfig) { c.Reviewers = 0 },
			wantMsg: "reviewers must be >= 1",
		},
		{
			name:    "negative concurrency",
			modify:  func(c *ResolvedConfig) { c.Concurrency = -1 },
			wantMsg: "concurrency must be >= 0",
		},
		{
			name:    "negative retries",
			modify:  func(c *ResolvedConfig) { c.Retries = -1 },
			wantMsg: "retries must be >= 0",
		},
		{
			name:    "zero timeout",
			modify:  func(c *ResolvedConfig) { c.Timeout = 0 },
			wantMsg: "timeout must be > 0",
		},
		{
			name:    "zero summarizer timeout",
			modify:  func(c *ResolvedConfig) { c.SummarizerTimeout = 0 },
			wantMsg: "summarizer_timeout must be > 0",
		},
		{
			name:    "zero fp filter timeout",
			modify:  func(c *ResolvedConfig) { c.FPFilterTimeout = 0 },
			wantMsg: "fp_filter_timeout must be > 0",
		},
		{
			name:    "empty reviewer agents",
			modify:  func(c *ResolvedConfig) { c.ReviewerAgents = []string{} },
			wantMsg: "reviewer_agents must not be empty",
		},
		{
			name:    "invalid reviewer agent",
			modify:  func(c *ResolvedConfig) { c.ReviewerAgents = []string{"unsupported"} },
			wantMsg: "unsupported agent",
		},
		{
			name:    "invalid summarizer agent",
			modify:  func(c *ResolvedConfig) { c.SummarizerAgent = "unsupported" },
			wantMsg: "summarizer_agent must be one of",
		},
		{
			name:    "fp threshold too low",
			modify:  func(c *ResolvedConfig) { c.FPThreshold = 0 },
			wantMsg: "fp_filter.threshold must be 1-100",
		},
		{
			name:    "fp threshold too high",
			modify:  func(c *ResolvedConfig) { c.FPThreshold = 101 },
			wantMsg: "fp_filter.threshold must be 1-100",
		},
		{
			name:    "invalid pr feedback agent",
			modify:  func(c *ResolvedConfig) { c.PRFeedbackAgent = "bad" },
			wantMsg: "pr_feedback.agent must be one of",
		},
		{
			name:    "invalid cross_check agent",
			modify:  func(c *ResolvedConfig) { c.CrossCheckAgent = "bogus" },
			wantMsg: "cross_check.agent contains unsupported agent",
		},
		{
			name:    "zero cross_check timeout",
			modify:  func(c *ResolvedConfig) { c.CrossCheckTimeout = 0 },
			wantMsg: "cross_check_timeout must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults
			tt.modify(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.wantMsg, err)
			}
		})
	}
}

func TestResolvedConfig_Validate_MultipleErrors(t *testing.T) {
	cfg := Defaults
	cfg.Reviewers = 0
	cfg.Retries = -1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "reviewers") || !strings.Contains(msg, "retries") {
		t.Errorf("expected both reviewers and retries errors, got: %v", err)
	}
}

func TestResolvedConfig_Validate_EmptyPRFeedbackAgent(t *testing.T) {
	cfg := Defaults
	cfg.PRFeedbackAgent = "" // empty means use summarizer agent, should be valid
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected empty pr_feedback.agent to be valid, got: %v", err)
	}
}

func TestResolvedConfig_Validate_EmptyReviewerAgents(t *testing.T) {
	cfg := Defaults
	cfg.ReviewerAgents = []string{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty reviewer_agents, got nil")
	}
	if !strings.Contains(err.Error(), "reviewer_agents must not be empty") {
		t.Errorf("expected 'reviewer_agents must not be empty' in error, got: %v", err)
	}
}

func TestResolvedConfig_Validate_EmptyCrossCheckAgent(t *testing.T) {
	cfg := Defaults
	cfg.CrossCheckAgent = "" // empty means fall back to summarizer
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected empty cross_check.agent to be valid, got: %v", err)
	}
}

// TestResolve_RejectsCrossCheckEnabledWithoutModel asserts the round-9 contract:
// when cross_check.enabled=true (the default) AND cross_check.model is empty,
// ValidateRuntime returns a fail-fast error directing the user to either supply
// a model or explicitly disable cross-check. ValidateRuntime (not ValidateAll)
// is used so YAML-only Config.Validate() does not false-positive on configs
// that legitimately defer the model to env/CLI.
func TestResolve_RejectsCrossCheckEnabledWithoutModel(t *testing.T) {
	wantSubstr := "cross_check.enabled=true requires cross_check.model"

	t.Run("defaults_only", func(t *testing.T) {
		// Bare defaults: enabled=true, model="" -> must reject at runtime.
		resolved := Resolve(&Config{}, EnvState{}, FlagState{}, Defaults)
		errs := resolved.ValidateRuntime()
		if !containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected error containing %q, got: %v", wantSubstr, errs)
		}
		// And ValidateAll must NOT include it (parse-time tests must stay green
		// for users who supply the model via env/CLI later).
		if containsSubstr(resolved.ValidateAll(), wantSubstr) {
			t.Fatalf("ValidateAll must not enforce runtime cross-check guard")
		}
	})

	t.Run("env_disable_only_no_model", func(t *testing.T) {
		// User disables via env (resolved.CrossCheckEnabled=false) -> ok even
		// when model is empty.
		env := EnvState{CrossCheckEnabled: false, CrossCheckEnabledSet: true}
		resolved := Resolve(&Config{}, env, FlagState{}, Defaults)
		if errs := resolved.ValidateRuntime(); containsSubstr(errs, wantSubstr) {
			t.Fatalf("did not expect cross-check error when disabled, got: %v", errs)
		}
	})

	t.Run("flag_supplies_model", func(t *testing.T) {
		flagState := FlagState{CrossCheckModelSet: true}
		flagValues := Defaults
		flagValues.CrossCheckModel = "gpt-flag"
		resolved := Resolve(&Config{}, EnvState{}, flagState, flagValues)
		if errs := resolved.ValidateRuntime(); containsSubstr(errs, wantSubstr) {
			t.Fatalf("did not expect cross-check error when --cross-check-model set, got: %v", errs)
		}
	})

	t.Run("env_supplies_model", func(t *testing.T) {
		env := EnvState{CrossCheckModel: "env-model", CrossCheckModelSet: true}
		resolved := Resolve(&Config{}, env, FlagState{}, Defaults)
		if errs := resolved.ValidateRuntime(); containsSubstr(errs, wantSubstr) {
			t.Fatalf("did not expect cross-check error when ACR_CROSS_CHECK_MODEL set, got: %v", errs)
		}
	})

	t.Run("yaml_supplies_model", func(t *testing.T) {
		cfg := &Config{CrossCheck: CrossCheckConfig{Model: strPtr("yaml-model")}}
		resolved := Resolve(cfg, EnvState{}, FlagState{}, Defaults)
		if errs := resolved.ValidateRuntime(); containsSubstr(errs, wantSubstr) {
			t.Fatalf("did not expect cross-check error when yaml model set, got: %v", errs)
		}
	})

	t.Run("whitespace_only_model_rejected", func(t *testing.T) {
		// "   " is not a valid model: contract uses TrimSpace.
		cfg := &Config{CrossCheck: CrossCheckConfig{Model: strPtr("   ")}}
		resolved := Resolve(cfg, EnvState{}, FlagState{}, Defaults)
		if errs := resolved.ValidateRuntime(); !containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected whitespace-only model to be rejected, got: %v", errs)
		}
	})
}

func containsSubstr(errs []string, want string) bool {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return true
		}
	}
	return false
}

func TestResolve_CrossCheckPrecedence(t *testing.T) {
	cfg := &Config{
		CrossCheck: CrossCheckConfig{
			Agent: strPtr("claude"),
			Model: strPtr("config-model"),
		},
		CrossCheckTimeout: durationPtr(3 * time.Minute),
	}
	envState := EnvState{
		CrossCheckAgent:      "gemini",
		CrossCheckAgentSet:   true,
		CrossCheckTimeout:    4 * time.Minute,
		CrossCheckTimeoutSet: true,
	}
	flagState := FlagState{
		CrossCheckModelSet: true,
	}
	flagValues := ResolvedConfig{
		CrossCheckModel: "flag-model",
	}

	result := Resolve(cfg, envState, flagState, flagValues)

	// env overrides config for agent
	if result.CrossCheckAgent != "gemini" {
		t.Errorf("expected env agent 'gemini', got %q", result.CrossCheckAgent)
	}
	// flag overrides config for model
	if result.CrossCheckModel != "flag-model" {
		t.Errorf("expected flag model 'flag-model', got %q", result.CrossCheckModel)
	}
	// env overrides config for timeout
	if result.CrossCheckTimeout != 4*time.Minute {
		t.Errorf("expected env timeout 4m, got %v", result.CrossCheckTimeout)
	}
}

func TestResolve_CrossCheckDefaultsWhenUnset(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.CrossCheckEnabled != Defaults.CrossCheckEnabled {
		t.Errorf("expected default cross_check enabled=%v, got %v", Defaults.CrossCheckEnabled, result.CrossCheckEnabled)
	}
	if result.CrossCheckTimeout != Defaults.CrossCheckTimeout {
		t.Errorf("expected default timeout %v, got %v", Defaults.CrossCheckTimeout, result.CrossCheckTimeout)
	}
	if result.CrossCheckAgent != "" {
		t.Errorf("expected empty default agent, got %q", result.CrossCheckAgent)
	}
}

func TestLoadEnvState_CrossCheck(t *testing.T) {
	t.Setenv("ACR_CROSS_CHECK", "false")
	t.Setenv("ACR_CROSS_CHECK_AGENT", "gemini")
	t.Setenv("ACR_CROSS_CHECK_MODEL", "env-model")
	t.Setenv("ACR_CROSS_CHECK_TIMEOUT", "7m")

	state, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if !state.CrossCheckEnabledSet || state.CrossCheckEnabled {
		t.Errorf("expected CrossCheckEnabled=false (set), got set=%v enabled=%v",
			state.CrossCheckEnabledSet, state.CrossCheckEnabled)
	}
	if !state.CrossCheckAgentSet || state.CrossCheckAgent != "gemini" {
		t.Errorf("expected agent=gemini (set), got set=%v agent=%q",
			state.CrossCheckAgentSet, state.CrossCheckAgent)
	}
	if !state.CrossCheckModelSet || state.CrossCheckModel != "env-model" {
		t.Errorf("expected model=env-model (set), got set=%v model=%q",
			state.CrossCheckModelSet, state.CrossCheckModel)
	}
	if !state.CrossCheckTimeoutSet || state.CrossCheckTimeout != 7*time.Minute {
		t.Errorf("expected timeout=7m (set), got set=%v timeout=%v",
			state.CrossCheckTimeoutSet, state.CrossCheckTimeout)
	}
}

// Tests for cross_check.agent multi-value validation

func baseResolvedConfig() ResolvedConfig {
	// Minimal valid ResolvedConfig so ValidateAll only fails on CrossCheckAgent.
	return ResolvedConfig{
		Reviewers:         1,
		Concurrency:       0,
		Base:              "main",
		Timeout:           10 * time.Minute,
		Retries:           0,
		SummarizerAgent:   "codex",
		FPThreshold:       50,
		CrossCheckTimeout: 5 * time.Minute,
	}
}

func TestValidateCrossCheckAgent_MultiValueAccepted(t *testing.T) {
	cfg := baseResolvedConfig()
	cfg.CrossCheckAgent = "codex,claude"
	errs := cfg.ValidateAll()
	for _, e := range errs {
		if strings.Contains(e, "cross_check.agent") {
			t.Errorf("unexpected cross_check.agent error: %s", e)
		}
	}
}

func TestValidateCrossCheckAgent_SingleValueAccepted(t *testing.T) {
	cfg := baseResolvedConfig()
	cfg.CrossCheckAgent = "codex"
	errs := cfg.ValidateAll()
	for _, e := range errs {
		if strings.Contains(e, "cross_check.agent") {
			t.Errorf("unexpected cross_check.agent error: %s", e)
		}
	}
}

func TestValidateCrossCheckAgent_EmptyAccepted(t *testing.T) {
	cfg := baseResolvedConfig()
	cfg.CrossCheckAgent = ""
	errs := cfg.ValidateAll()
	for _, e := range errs {
		if strings.Contains(e, "cross_check.agent") {
			t.Errorf("unexpected cross_check.agent error: %s", e)
		}
	}
}

func TestValidateCrossCheckAgent_UnknownTokenRejected(t *testing.T) {
	cfg := baseResolvedConfig()
	cfg.CrossCheckAgent = "codex,foobar"
	errs := cfg.ValidateAll()
	found := false
	for _, e := range errs {
		if strings.Contains(e, "foobar") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning %q, got: %v", "foobar", errs)
	}
}

func TestValidateCrossCheckAgent_EmptyTokenRejected(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"double comma", "codex,,claude"},
		{"trailing whitespace token", "codex, "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseResolvedConfig()
			cfg.CrossCheckAgent = tt.value
			errs := cfg.ValidateAll()
			found := false
			for _, e := range errs {
				if strings.Contains(e, "cross_check.agent") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected cross_check.agent error for %q, got: %v", tt.value, errs)
			}
		})
	}
}

func TestValidateCrossCheckAgent_TrimsWhitespace(t *testing.T) {
	cfg := baseResolvedConfig()
	cfg.CrossCheckAgent = " codex , claude "
	errs := cfg.ValidateAll()
	for _, e := range errs {
		if strings.Contains(e, "cross_check.agent") {
			t.Errorf("unexpected cross_check.agent error (whitespace should be trimmed): %s", e)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

// --- AutoPhase config resolution tests ---

func TestResolve_AutoPhaseDefaultsToTrue(t *testing.T) {
	// No flag, no env, no yaml → AutoPhase must be true (new default).
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if !result.AutoPhase {
		t.Errorf("expected AutoPhase=true as default, got false")
	}
}

func TestResolve_AutoPhaseEnvFalse(t *testing.T) {
	// ACR_AUTO_PHASE=false → AutoPhase=false.
	t.Setenv("ACR_AUTO_PHASE", "false")
	env, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	result := Resolve(&Config{}, env, FlagState{}, ResolvedConfig{})
	if result.AutoPhase {
		t.Errorf("expected AutoPhase=false from env, got true")
	}
}

func TestResolve_AutoPhaseYamlFalse(t *testing.T) {
	// yaml auto_phase: false → AutoPhase=false.
	cfg := &Config{AutoPhase: boolPtr(false)}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.AutoPhase {
		t.Errorf("expected AutoPhase=false from yaml, got true")
	}
}

func TestResolve_AutoPhaseFlagOverridesYaml(t *testing.T) {
	// yaml false + flag --auto-phase → AutoPhase=true.
	cfgFalse := &Config{AutoPhase: boolPtr(false)}
	result := Resolve(cfgFalse, EnvState{}, FlagState{AutoPhaseSet: true}, ResolvedConfig{AutoPhase: true})
	if !result.AutoPhase {
		t.Errorf("expected flag --auto-phase to override yaml false, got false")
	}

	// flag --no-auto-phase + yaml true → AutoPhase=false.
	cfgTrue := &Config{AutoPhase: boolPtr(true)}
	result = Resolve(cfgTrue, EnvState{}, FlagState{AutoPhaseSet: true}, ResolvedConfig{AutoPhase: false})
	if result.AutoPhase {
		t.Errorf("expected flag --no-auto-phase to override yaml true, got true")
	}
}

// --- Strict config resolution tests ---

func TestResolve_StrictDefaultsToFalse(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.Strict {
		t.Errorf("expected Strict=false as default, got true")
	}
}

func TestResolve_StrictEnv(t *testing.T) {
	t.Setenv("ACR_STRICT", "true")
	env, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	result := Resolve(&Config{}, env, FlagState{}, ResolvedConfig{})
	if !result.Strict {
		t.Errorf("expected Strict=true from env, got false")
	}
}

func TestResolve_StrictFlag(t *testing.T) {
	// Flag overrides env.
	t.Setenv("ACR_STRICT", "true")
	env, _ := LoadEnvState()
	result := Resolve(&Config{}, env, FlagState{StrictSet: true}, ResolvedConfig{Strict: false})
	if result.Strict {
		t.Errorf("expected flag Strict=false to override env true, got true")
	}

	// Explicit flag true without env.
	result = Resolve(&Config{}, EnvState{}, FlagState{StrictSet: true}, ResolvedConfig{Strict: true})
	if !result.Strict {
		t.Errorf("expected flag Strict=true, got false")
	}
}

func TestLoadEnvState_AutoPhaseParsing(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		wantSet  bool
		wantVal  bool
		wantWarn bool
	}{
		{"true", "true", true, true, false},
		{"1", "1", true, true, false},
		{"yes", "yes", true, true, false},
		{"false", "false", true, false, false},
		{"0", "0", true, false, false},
		{"no", "no", true, false, false},
		{"invalid", "maybe", false, false, true},
		{"empty (unset)", "", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv("ACR_AUTO_PHASE", tt.envVal)
			} else {
				t.Setenv("ACR_AUTO_PHASE", "")
			}
			state, warnings := LoadEnvState()
			if tt.wantWarn {
				if len(warnings) == 0 {
					t.Errorf("expected warning for ACR_AUTO_PHASE=%q, got none", tt.envVal)
				}
			} else {
				if len(warnings) != 0 {
					t.Errorf("unexpected warnings for ACR_AUTO_PHASE=%q: %v", tt.envVal, warnings)
				}
			}
			if state.AutoPhaseSet != tt.wantSet {
				t.Errorf("AutoPhaseSet: got %v, want %v", state.AutoPhaseSet, tt.wantSet)
			}
			if tt.wantSet && state.AutoPhase != tt.wantVal {
				t.Errorf("AutoPhase: got %v, want %v", state.AutoPhase, tt.wantVal)
			}
		})
	}
}

func TestConfig_ModelsSection_Parses(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  defaults:
    reviewer:       { model: gpt-5.4-mini, effort: medium }
    arch_reviewer:  { model: gpt-5.4,      effort: high }
    diff_reviewer:  { model: gpt-5.4-mini, effort: medium }
    summarizer:     { model: gpt-5.4,      effort: high }
    fp_filter:      { model: gpt-5.4-mini, effort: low }
    cross_check:    { model: gpt-5.4,      effort: medium }
    pr_feedback:    { model: gpt-5.4-mini }
  sizes:
    small:
      reviewer: { model: gpt-5.4-mini, effort: low }
    medium:
      reviewer: { model: gpt-5.4-mini, effort: medium }
    large:
      reviewer:      { model: gpt-5.4,      effort: high }
      arch_reviewer: { model: gpt-5.4,      effort: high }
      diff_reviewer: { model: gpt-5.4,      effort: medium }
      summarizer:    { model: gpt-5.4,      effort: high }
  agents:
    codex:
      reviewer:      { model: gpt-5.4-mini, effort: medium }
      arch_reviewer: { effort: high }
      diff_reviewer: { effort: medium }
    claude:
      reviewer: { model: sonnet-4-6,   effort: max }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	m := result.Config.Models

	// defaults
	if m.Defaults.Reviewer == nil {
		t.Fatal("defaults.reviewer should not be nil")
	}
	if m.Defaults.Reviewer.Model != "gpt-5.4-mini" {
		t.Errorf("defaults.reviewer.model: got %q, want %q", m.Defaults.Reviewer.Model, "gpt-5.4-mini")
	}
	if m.Defaults.Reviewer.Effort != "medium" {
		t.Errorf("defaults.reviewer.effort: got %q, want %q", m.Defaults.Reviewer.Effort, "medium")
	}
	if m.Defaults.ArchReviewer == nil {
		t.Fatal("defaults.arch_reviewer should not be nil")
	}
	if m.Defaults.ArchReviewer.Model != "gpt-5.4" || m.Defaults.ArchReviewer.Effort != "high" {
		t.Errorf("defaults.arch_reviewer: got %+v, want {gpt-5.4, high}", *m.Defaults.ArchReviewer)
	}
	if m.Defaults.DiffReviewer == nil {
		t.Fatal("defaults.diff_reviewer should not be nil")
	}
	if m.Defaults.DiffReviewer.Model != "gpt-5.4-mini" || m.Defaults.DiffReviewer.Effort != "medium" {
		t.Errorf("defaults.diff_reviewer: got %+v, want {gpt-5.4-mini, medium}", *m.Defaults.DiffReviewer)
	}
	if m.Defaults.Summarizer == nil {
		t.Fatal("defaults.summarizer should not be nil")
	}
	if m.Defaults.Summarizer.Model != "gpt-5.4" {
		t.Errorf("defaults.summarizer.model: got %q, want %q", m.Defaults.Summarizer.Model, "gpt-5.4")
	}
	if m.Defaults.FPFilter == nil {
		t.Fatal("defaults.fp_filter should not be nil")
	}
	if m.Defaults.FPFilter.Effort != "low" {
		t.Errorf("defaults.fp_filter.effort: got %q, want %q", m.Defaults.FPFilter.Effort, "low")
	}
	if m.Defaults.CrossCheck == nil {
		t.Fatal("defaults.cross_check should not be nil")
	}
	if m.Defaults.PRFeedback == nil {
		t.Fatal("defaults.pr_feedback should not be nil")
	}
	if m.Defaults.PRFeedback.Effort != "" {
		t.Errorf("defaults.pr_feedback.effort: expected empty, got %q", m.Defaults.PRFeedback.Effort)
	}

	// sizes
	small, ok := m.Sizes["small"]
	if !ok {
		t.Fatal("sizes.small not found")
	}
	if small.Reviewer == nil {
		t.Fatal("sizes.small.reviewer should not be nil")
	}
	if small.Reviewer.Effort != "low" {
		t.Errorf("sizes.small.reviewer.effort: got %q, want %q", small.Reviewer.Effort, "low")
	}
	large, ok := m.Sizes["large"]
	if !ok {
		t.Fatal("sizes.large not found")
	}
	if large.Summarizer == nil {
		t.Fatal("sizes.large.summarizer should not be nil")
	}

	// agents
	codex, ok := m.Agents["codex"]
	if !ok {
		t.Fatal("agents.codex not found")
	}
	if codex.Reviewer == nil {
		t.Fatal("agents.codex.reviewer should not be nil")
	}
	if codex.Reviewer.Model != "gpt-5.4-mini" {
		t.Errorf("agents.codex.reviewer.model: got %q, want %q", codex.Reviewer.Model, "gpt-5.4-mini")
	}
	if codex.ArchReviewer == nil || codex.ArchReviewer.Effort != "high" {
		t.Errorf("agents.codex.arch_reviewer.effort: got %+v, want effort=high", codex.ArchReviewer)
	}
	if codex.DiffReviewer == nil || codex.DiffReviewer.Effort != "medium" {
		t.Errorf("agents.codex.diff_reviewer.effort: got %+v, want effort=medium", codex.DiffReviewer)
	}
	claude, ok := m.Agents["claude"]
	if !ok {
		t.Fatal("agents.claude not found")
	}
	if claude.Reviewer == nil {
		t.Fatal("agents.claude.reviewer should not be nil")
	}
	if claude.Reviewer.Effort != "max" {
		t.Errorf("agents.claude.reviewer.effort: got %q, want %q", claude.Reviewer.Effort, "max")
	}
}

func TestConfig_ModelsSection_UnknownKey_Warns(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  defaults:
    unknown_role: { model: gpt-5.4-mini }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warning for unknown role key, got none")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "unknown_role") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning 'unknown_role', got: %v", result.Warnings)
	}
}

func TestConfig_ModelsSection_AgentsMustBeSupported(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  agents:
    unknown_agent:
      reviewer: { model: gpt-5.4-mini }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for unsupported agent in models.agents, got nil")
	}
	if !strings.Contains(err.Error(), "unknown_agent") {
		t.Errorf("expected error mentioning 'unknown_agent', got: %v", err)
	}
}

func TestConfig_ModelsSection_EmptySectionOK(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 3
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.Config.Models
	if m.Defaults.Reviewer != nil {
		t.Error("expected Models.Defaults.Reviewer to be nil when models section absent")
	}
	if len(m.Sizes) != 0 {
		t.Errorf("expected Models.Sizes to be empty, got %v", m.Sizes)
	}
	if len(m.Agents) != 0 {
		t.Errorf("expected Models.Agents to be empty, got %v", m.Agents)
	}
	if result.Config.Reviewers == nil || *result.Config.Reviewers != 3 {
		t.Error("expected Reviewers=3 to still be set correctly")
	}
}

// --- F#1: CLI-vs-legacy model precedence markers ---

func TestResolve_ReviewerModelFromCLI_FlagSet(t *testing.T) {
	r := Resolve(&Config{}, EnvState{}, FlagState{ReviewerModelSet: true}, ResolvedConfig{ReviewerModel: "gpt-cli"})
	if !r.ReviewerModelFromCLI {
		t.Errorf("expected ReviewerModelFromCLI=true when flag is set")
	}
	if r.ReviewerModel != "gpt-cli" {
		t.Errorf("expected ReviewerModel=gpt-cli, got %q", r.ReviewerModel)
	}
}

func TestResolve_ReviewerModelFromCLI_EnvSet(t *testing.T) {
	r := Resolve(&Config{}, EnvState{ReviewerModel: "gpt-env", ReviewerModelSet: true}, FlagState{}, ResolvedConfig{})
	if !r.ReviewerModelFromCLI {
		t.Errorf("expected ReviewerModelFromCLI=true when env is set")
	}
}

func TestResolve_ReviewerModelFromCLI_ConfigOnly(t *testing.T) {
	s := "gpt-cfg"
	r := Resolve(&Config{ReviewerModel: &s}, EnvState{}, FlagState{}, ResolvedConfig{})
	if r.ReviewerModelFromCLI {
		t.Errorf("expected ReviewerModelFromCLI=false when only config sets ReviewerModel")
	}
	if r.ReviewerModel != "gpt-cfg" {
		t.Errorf("expected ReviewerModel=gpt-cfg, got %q", r.ReviewerModel)
	}
}

func TestResolve_SummarizerModelFromCLI(t *testing.T) {
	r := Resolve(&Config{}, EnvState{}, FlagState{SummarizerModelSet: true}, ResolvedConfig{SummarizerModel: "sum-cli"})
	if !r.SummarizerModelFromCLI {
		t.Errorf("expected SummarizerModelFromCLI=true when flag is set")
	}
}

func TestResolve_CrossCheckModelFromCLI(t *testing.T) {
	r := Resolve(&Config{}, EnvState{CrossCheckModel: "cc-env", CrossCheckModelSet: true}, FlagState{}, ResolvedConfig{})
	if !r.CrossCheckModelFromCLI {
		t.Errorf("expected CrossCheckModelFromCLI=true when env is set")
	}
}

// --- F#3: models.sizes key + effort validation ---

func TestConfig_ModelsSizes_UnknownSizeKeyRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  sizes:
    larg:
      reviewer: { model: gpt-5.4 }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for unknown size key, got nil")
	}
	if !strings.Contains(err.Error(), "unknown size key") {
		t.Errorf("expected error to mention 'unknown size key', got: %v", err)
	}
	if !strings.Contains(err.Error(), `"larg"`) {
		t.Errorf("expected error to mention the bad key %q, got: %v", "larg", err)
	}
}

func TestConfig_ModelsEffort_CodexXhighRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  agents:
    codex:
      reviewer: { model: gpt-5.4, effort: xhigh }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for xhigh on codex, got nil")
	}
	if !strings.Contains(err.Error(), "xhigh") {
		t.Errorf("expected error to mention 'xhigh', got: %v", err)
	}
}

func TestConfig_ModelsEffort_GeminiEffortRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  agents:
    gemini:
      reviewer: { model: gemini-2.5-pro, effort: low }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for effort on gemini, got nil")
	}
	if !strings.Contains(err.Error(), "effort") {
		t.Errorf("expected error to mention 'effort', got: %v", err)
	}
}

func TestConfig_ModelsEffort_DefaultsXhighAccepted(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  defaults:
    reviewer: { model: gpt-5.4, effort: xhigh }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("expected no error for xhigh in defaults (loose set), got: %v", err)
	}
	if result.Config.Models.Defaults.Reviewer == nil || result.Config.Models.Defaults.Reviewer.Effort != "xhigh" {
		t.Errorf("expected defaults.reviewer.effort=xhigh, got %+v", result.Config.Models.Defaults.Reviewer)
	}
}

func TestConfig_ModelsEffort_ClaudeMaxAccepted(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  agents:
    claude:
      reviewer: { model: sonnet-4-6, effort: max }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("expected no error for claude.effort=max, got: %v", err)
	}
}

func TestConfig_ModelsEffort_CaseInsensitive_Defaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  defaults:
    reviewer: { model: gpt-5.4, effort: High }
    summarizer: { model: gpt-5.4, effort: XHIGH }
    fp_filter: { model: gpt-5.4, effort: Low }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("expected case-insensitive effort to be accepted in defaults, got: %v", err)
	}
	if result.Config.Models.Defaults.Reviewer == nil || result.Config.Models.Defaults.Reviewer.Effort != "High" {
		t.Errorf("expected defaults.reviewer.effort=High, got %+v", result.Config.Models.Defaults.Reviewer)
	}
	if result.Config.Models.Defaults.Summarizer == nil || result.Config.Models.Defaults.Summarizer.Effort != "XHIGH" {
		t.Errorf("expected defaults.summarizer.effort=XHIGH, got %+v", result.Config.Models.Defaults.Summarizer)
	}
}

func TestConfig_ModelsEffort_CaseInsensitive_Sizes(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  sizes:
    small:
      reviewer: { model: gpt-5.4-mini, effort: LOW }
    large:
      arch_reviewer: { model: gpt-5.4, effort: MediuM }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("expected case-insensitive effort to be accepted in sizes, got: %v", err)
	}
	small := result.Config.Models.Sizes["small"]
	if small.Reviewer == nil || small.Reviewer.Effort != "LOW" {
		t.Errorf("expected sizes.small.reviewer.effort=LOW, got %+v", small.Reviewer)
	}
	large := result.Config.Models.Sizes["large"]
	if large.ArchReviewer == nil || large.ArchReviewer.Effort != "MediuM" {
		t.Errorf("expected sizes.large.arch_reviewer.effort=MediuM, got %+v", large.ArchReviewer)
	}
}

func TestConfig_ModelsEffort_CaseInsensitive_Agents(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  agents:
    codex:
      reviewer: { model: gpt-5.4, effort: MEDIUM }
    claude:
      reviewer: { model: sonnet-4-6, effort: Max }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("expected case-insensitive effort to be accepted in agents, got: %v", err)
	}
	codex := result.Config.Models.Agents["codex"]
	if codex.Reviewer == nil || codex.Reviewer.Effort != "MEDIUM" {
		t.Errorf("expected agents.codex.reviewer.effort=MEDIUM, got %+v", codex.Reviewer)
	}
	claude := result.Config.Models.Agents["claude"]
	if claude.Reviewer == nil || claude.Reviewer.Effort != "Max" {
		t.Errorf("expected agents.claude.reviewer.effort=Max, got %+v", claude.Reviewer)
	}
}

func TestConfig_ModelsEffort_CaseInsensitive_StillRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `models:
  defaults:
    reviewer: { model: gpt-5.4, effort: ULTRA }
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for invalid effort 'ULTRA', got nil")
	}
	if !strings.Contains(err.Error(), "ULTRA") {
		t.Errorf("expected error to mention 'ULTRA', got: %v", err)
	}
}

// --- Round-9: per-phase reviewer agent override tests ---

func TestResolve_ArchReviewerAgent_FromYAML(t *testing.T) {
	cfg := &Config{
		ReviewerAgents:    []string{"codex", "claude", "gemini"},
		ArchReviewerAgent: strPtr("claude"),
	}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.ArchReviewerAgent != "claude" {
		t.Errorf("expected ArchReviewerAgent='claude', got %q", result.ArchReviewerAgent)
	}
	if len(result.ReviewerAgents) != 3 {
		t.Errorf("reviewer_agents should be preserved, got %v", result.ReviewerAgents)
	}
}

func TestResolve_DiffReviewerAgents_FromYAML(t *testing.T) {
	cfg := &Config{
		ReviewerAgents:     []string{"codex", "claude", "gemini"},
		DiffReviewerAgents: []string{"codex", "claude"},
	}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if len(result.DiffReviewerAgents) != 2 {
		t.Fatalf("expected 2 diff reviewer agents, got %d", len(result.DiffReviewerAgents))
	}
	if result.DiffReviewerAgents[0] != "codex" || result.DiffReviewerAgents[1] != "claude" {
		t.Errorf("expected [codex claude], got %v", result.DiffReviewerAgents)
	}
}

func TestResolve_ArchReviewerAgent_CLIOverridesYAML(t *testing.T) {
	cfg := &Config{
		ReviewerAgents:    []string{"codex"},
		ArchReviewerAgent: strPtr("claude"),
	}
	envState := EnvState{ArchReviewerAgent: "gemini", ArchReviewerAgentSet: true}
	flagState := FlagState{ArchReviewerAgentSet: true}
	flagValues := ResolvedConfig{ArchReviewerAgent: "codex"}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.ArchReviewerAgent != "codex" {
		t.Errorf("expected CLI value 'codex' to win, got %q", result.ArchReviewerAgent)
	}
}

func TestResolve_DiffReviewerAgents_EnvOverridesYAML(t *testing.T) {
	cfg := &Config{
		ReviewerAgents:     []string{"codex"},
		DiffReviewerAgents: []string{"claude"},
	}
	envState := EnvState{
		DiffReviewerAgents:    []string{"gemini", "codex"},
		DiffReviewerAgentsSet: true,
	}
	result := Resolve(cfg, envState, FlagState{}, ResolvedConfig{})

	if len(result.DiffReviewerAgents) != 2 {
		t.Fatalf("expected 2 diff reviewer agents, got %v", result.DiffReviewerAgents)
	}
	if result.DiffReviewerAgents[0] != "gemini" {
		t.Errorf("expected env value 'gemini' first, got %q", result.DiffReviewerAgents[0])
	}
}

func TestResolve_DiffReviewerAgents_DefaultsEmpty(t *testing.T) {
	cfg := &Config{ReviewerAgents: []string{"codex"}}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if len(result.DiffReviewerAgents) != 0 {
		t.Errorf("expected empty DiffReviewerAgents when unset, got %v", result.DiffReviewerAgents)
	}
	if result.ArchReviewerAgent != "" {
		t.Errorf("expected empty ArchReviewerAgent when unset, got %q", result.ArchReviewerAgent)
	}
}

func TestResolve_ArchReviewerAgent_RejectsUnsupportedAgent(t *testing.T) {
	cfg := &Config{
		ReviewerAgents:    []string{"codex"},
		ArchReviewerAgent: strPtr("notarealagent"),
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for unsupported arch_reviewer_agent")
	} else if !strings.Contains(err.Error(), "arch_reviewer_agent") {
		t.Errorf("expected error mentioning arch_reviewer_agent, got: %v", err)
	}
}

func TestResolve_DiffReviewerAgents_RejectsUnsupportedAgent(t *testing.T) {
	cfg := &Config{
		ReviewerAgents:     []string{"codex"},
		DiffReviewerAgents: []string{"codex", "notarealagent"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for unsupported diff_reviewer_agents entry")
	} else if !strings.Contains(err.Error(), "diff_reviewer_agents") {
		t.Errorf("expected error mentioning diff_reviewer_agents, got: %v", err)
	}
}

func TestLoadEnvState_ArchReviewerAgent(t *testing.T) {
	original := os.Getenv("ACR_ARCH_REVIEWER_AGENT")
	defer func() {
		if original != "" {
			os.Setenv("ACR_ARCH_REVIEWER_AGENT", original)
		} else {
			os.Unsetenv("ACR_ARCH_REVIEWER_AGENT")
		}
	}()

	os.Setenv("ACR_ARCH_REVIEWER_AGENT", "claude")
	state, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !state.ArchReviewerAgentSet {
		t.Error("expected ArchReviewerAgentSet=true")
	}
	if state.ArchReviewerAgent != "claude" {
		t.Errorf("expected ArchReviewerAgent='claude', got %q", state.ArchReviewerAgent)
	}
}

func TestLoadEnvState_DiffReviewerAgents(t *testing.T) {
	original := os.Getenv("ACR_DIFF_REVIEWER_AGENTS")
	defer func() {
		if original != "" {
			os.Setenv("ACR_DIFF_REVIEWER_AGENTS", original)
		} else {
			os.Unsetenv("ACR_DIFF_REVIEWER_AGENTS")
		}
	}()

	os.Setenv("ACR_DIFF_REVIEWER_AGENTS", "codex, claude")
	state, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !state.DiffReviewerAgentsSet {
		t.Error("expected DiffReviewerAgentsSet=true")
	}
	if len(state.DiffReviewerAgents) != 2 || state.DiffReviewerAgents[0] != "codex" || state.DiffReviewerAgents[1] != "claude" {
		t.Errorf("expected [codex claude], got %v", state.DiffReviewerAgents)
	}
}

// ---------------------------------------------------------------------------
// Tests for large_diff_reviewers and medium_diff_reviewers (auto-phase reviewer knobs)
// ---------------------------------------------------------------------------

func TestResolve_LargeDiffReviewers_FromYAML(t *testing.T) {
	cfg := &Config{LargeDiffReviewers: ptr(7)}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.LargeDiffReviewers != 7 {
		t.Errorf("expected large_diff_reviewers=7 from yaml, got %d", result.LargeDiffReviewers)
	}
}

func TestResolve_LargeDiffReviewers_EnvOverridesYAML(t *testing.T) {
	cfg := &Config{LargeDiffReviewers: ptr(7)}
	envState := EnvState{LargeDiffReviewers: 9, LargeDiffReviewersSet: true}
	result := Resolve(cfg, envState, FlagState{}, ResolvedConfig{})
	if result.LargeDiffReviewers != 9 {
		t.Errorf("expected env large_diff_reviewers=9 to override yaml, got %d", result.LargeDiffReviewers)
	}
}

func TestResolve_LargeDiffReviewers_CLIOverridesEnv(t *testing.T) {
	cfg := &Config{LargeDiffReviewers: ptr(7)}
	envState := EnvState{LargeDiffReviewers: 9, LargeDiffReviewersSet: true}
	flagState := FlagState{LargeDiffReviewersSet: true}
	flagValues := ResolvedConfig{LargeDiffReviewers: 12}
	result := Resolve(cfg, envState, flagState, flagValues)
	if result.LargeDiffReviewers != 12 {
		t.Errorf("expected CLI large_diff_reviewers=12 to override env, got %d", result.LargeDiffReviewers)
	}
}

func TestResolve_LargeDiffReviewers_DefaultsTo4(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.LargeDiffReviewers != 4 {
		t.Errorf("expected default large_diff_reviewers=4, got %d", result.LargeDiffReviewers)
	}
	if Defaults.LargeDiffReviewers != 4 {
		t.Errorf("expected Defaults.LargeDiffReviewers=4, got %d", Defaults.LargeDiffReviewers)
	}
}

func TestResolve_MediumDiffReviewers_FromYAML(t *testing.T) {
	cfg := &Config{MediumDiffReviewers: ptr(5)}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.MediumDiffReviewers != 5 {
		t.Errorf("expected medium_diff_reviewers=5 from yaml, got %d", result.MediumDiffReviewers)
	}
}

func TestResolve_MediumDiffReviewers_EnvOverridesYAML(t *testing.T) {
	cfg := &Config{MediumDiffReviewers: ptr(5)}
	envState := EnvState{MediumDiffReviewers: 8, MediumDiffReviewersSet: true}
	result := Resolve(cfg, envState, FlagState{}, ResolvedConfig{})
	if result.MediumDiffReviewers != 8 {
		t.Errorf("expected env medium_diff_reviewers=8 to override yaml, got %d", result.MediumDiffReviewers)
	}
}

func TestResolve_MediumDiffReviewers_CLIOverridesEnv(t *testing.T) {
	cfg := &Config{MediumDiffReviewers: ptr(5)}
	envState := EnvState{MediumDiffReviewers: 8, MediumDiffReviewersSet: true}
	flagState := FlagState{MediumDiffReviewersSet: true}
	flagValues := ResolvedConfig{MediumDiffReviewers: 11}
	result := Resolve(cfg, envState, flagState, flagValues)
	if result.MediumDiffReviewers != 11 {
		t.Errorf("expected CLI medium_diff_reviewers=11 to override env, got %d", result.MediumDiffReviewers)
	}
}

func TestResolve_MediumDiffReviewers_DefaultsTo2(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.MediumDiffReviewers != 2 {
		t.Errorf("expected default medium_diff_reviewers=2, got %d", result.MediumDiffReviewers)
	}
	if Defaults.MediumDiffReviewers != 2 {
		t.Errorf("expected Defaults.MediumDiffReviewers=2, got %d", Defaults.MediumDiffReviewers)
	}
}

func TestValidate_RejectsZeroLargeDiffReviewers(t *testing.T) {
	cfg := Defaults
	cfg.LargeDiffReviewers = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for large_diff_reviewers=0, got nil")
	}
	if !strings.Contains(err.Error(), "large_diff_reviewers must be >= 1") {
		t.Errorf("expected 'large_diff_reviewers must be >= 1' in error, got: %v", err)
	}
}

func TestValidate_RejectsZeroMediumDiffReviewers(t *testing.T) {
	cfg := Defaults
	cfg.MediumDiffReviewers = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for medium_diff_reviewers=0, got nil")
	}
	if !strings.Contains(err.Error(), "medium_diff_reviewers must be >= 1") {
		t.Errorf("expected 'medium_diff_reviewers must be >= 1' in error, got: %v", err)
	}
}

func TestLoadEnvState_LargeDiffReviewers(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_LARGE_DIFF_REVIEWERS", "6")
	state, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !state.LargeDiffReviewersSet {
		t.Error("expected LargeDiffReviewersSet=true")
	}
	if state.LargeDiffReviewers != 6 {
		t.Errorf("expected LargeDiffReviewers=6, got %d", state.LargeDiffReviewers)
	}
}

func TestLoadEnvState_LargeDiffReviewers_Malformed(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_LARGE_DIFF_REVIEWERS", "abc")
	state, warnings := LoadEnvState()
	if state.LargeDiffReviewersSet {
		t.Error("expected LargeDiffReviewersSet=false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_LARGE_DIFF_REVIEWERS") {
		t.Errorf("expected warning about ACR_LARGE_DIFF_REVIEWERS, got %v", warnings)
	}
}

func TestLoadEnvState_MediumDiffReviewers(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_MEDIUM_DIFF_REVIEWERS", "3")
	state, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !state.MediumDiffReviewersSet {
		t.Error("expected MediumDiffReviewersSet=true")
	}
	if state.MediumDiffReviewers != 3 {
		t.Errorf("expected MediumDiffReviewers=3, got %d", state.MediumDiffReviewers)
	}
}

func TestLoadEnvState_MediumDiffReviewers_Malformed(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_MEDIUM_DIFF_REVIEWERS", "xyz")
	state, warnings := LoadEnvState()
	if state.MediumDiffReviewersSet {
		t.Error("expected MediumDiffReviewersSet=false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_MEDIUM_DIFF_REVIEWERS") {
		t.Errorf("expected warning about ACR_MEDIUM_DIFF_REVIEWERS, got %v", warnings)
	}
}

func TestResolve_SmallDiffReviewers_FromYAML(t *testing.T) {
	cfg := &Config{SmallDiffReviewers: ptr(3)}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.SmallDiffReviewers != 3 {
		t.Errorf("expected small_diff_reviewers=3 from yaml, got %d", result.SmallDiffReviewers)
	}
}

func TestResolve_SmallDiffReviewers_EnvOverridesYAML(t *testing.T) {
	cfg := &Config{SmallDiffReviewers: ptr(3)}
	envState := EnvState{SmallDiffReviewers: 7, SmallDiffReviewersSet: true}
	result := Resolve(cfg, envState, FlagState{}, ResolvedConfig{})
	if result.SmallDiffReviewers != 7 {
		t.Errorf("expected env small_diff_reviewers=7 to override yaml, got %d", result.SmallDiffReviewers)
	}
}

func TestResolve_SmallDiffReviewers_CLIOverridesEnv(t *testing.T) {
	cfg := &Config{SmallDiffReviewers: ptr(3)}
	envState := EnvState{SmallDiffReviewers: 7, SmallDiffReviewersSet: true}
	flagState := FlagState{SmallDiffReviewersSet: true}
	flagValues := ResolvedConfig{SmallDiffReviewers: 10}
	result := Resolve(cfg, envState, flagState, flagValues)
	if result.SmallDiffReviewers != 10 {
		t.Errorf("expected CLI small_diff_reviewers=10 to override env, got %d", result.SmallDiffReviewers)
	}
}

func TestResolve_SmallDiffReviewers_DefaultsTo1(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.SmallDiffReviewers != 1 {
		t.Errorf("expected default small_diff_reviewers=1, got %d", result.SmallDiffReviewers)
	}
	if Defaults.SmallDiffReviewers != 1 {
		t.Errorf("expected Defaults.SmallDiffReviewers=1, got %d", Defaults.SmallDiffReviewers)
	}
}

func TestValidate_RejectsZeroSmallDiffReviewers(t *testing.T) {
	cfg := Defaults
	cfg.SmallDiffReviewers = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for small_diff_reviewers=0, got nil")
	}
	if !strings.Contains(err.Error(), "small_diff_reviewers must be >= 1") {
		t.Errorf("expected 'small_diff_reviewers must be >= 1' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// min_large_diff_reviewers / min_medium_diff_reviewers (yaml-only floor keys)
// ---------------------------------------------------------------------------

func TestResolve_MinLargeDiffReviewers_DefaultsTo2(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.MinLargeDiffReviewers != 2 {
		t.Errorf("expected default min_large_diff_reviewers=2, got %d", result.MinLargeDiffReviewers)
	}
	if Defaults.MinLargeDiffReviewers != 2 {
		t.Errorf("expected Defaults.MinLargeDiffReviewers=2, got %d", Defaults.MinLargeDiffReviewers)
	}
}

func TestResolve_MinLargeDiffReviewers_FromYAML(t *testing.T) {
	cfg := &Config{MinLargeDiffReviewers: ptr(3)}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.MinLargeDiffReviewers != 3 {
		t.Errorf("expected min_large_diff_reviewers=3 from yaml, got %d", result.MinLargeDiffReviewers)
	}
}

func TestResolve_MinMediumDiffReviewers_DefaultsTo2(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.MinMediumDiffReviewers != 2 {
		t.Errorf("expected default min_medium_diff_reviewers=2, got %d", result.MinMediumDiffReviewers)
	}
	if Defaults.MinMediumDiffReviewers != 2 {
		t.Errorf("expected Defaults.MinMediumDiffReviewers=2, got %d", Defaults.MinMediumDiffReviewers)
	}
}

func TestResolve_MinMediumDiffReviewers_FromYAML(t *testing.T) {
	cfg := &Config{MinMediumDiffReviewers: ptr(4)}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if result.MinMediumDiffReviewers != 4 {
		t.Errorf("expected min_medium_diff_reviewers=4 from yaml, got %d", result.MinMediumDiffReviewers)
	}
}

func TestValidate_RejectsMinLargeDiffReviewersBelow2(t *testing.T) {
	cfg := Defaults
	cfg.MinLargeDiffReviewers = 1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for min_large_diff_reviewers=1, got nil")
	}
	if !strings.Contains(err.Error(), "min_large_diff_reviewers must be >= 2") {
		t.Errorf("expected 'min_large_diff_reviewers must be >= 2' in error, got: %v", err)
	}
}

func TestValidate_RejectsMinMediumDiffReviewersBelow2(t *testing.T) {
	cfg := Defaults
	cfg.MinMediumDiffReviewers = 1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for min_medium_diff_reviewers=1, got nil")
	}
	if !strings.Contains(err.Error(), "min_medium_diff_reviewers must be >= 2") {
		t.Errorf("expected 'min_medium_diff_reviewers must be >= 2' in error, got: %v", err)
	}
}

func TestLoadEnvState_SmallDiffReviewers(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_SMALL_DIFF_REVIEWERS", "4")
	state, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !state.SmallDiffReviewersSet {
		t.Error("expected SmallDiffReviewersSet=true")
	}
	if state.SmallDiffReviewers != 4 {
		t.Errorf("expected SmallDiffReviewers=4, got %d", state.SmallDiffReviewers)
	}
}

func TestLoadEnvState_SmallDiffReviewers_Malformed(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_SMALL_DIFF_REVIEWERS", "bad")
	state, warnings := LoadEnvState()
	if state.SmallDiffReviewersSet {
		t.Error("expected SmallDiffReviewersSet=false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_SMALL_DIFF_REVIEWERS") {
		t.Errorf("expected warning about ACR_SMALL_DIFF_REVIEWERS, got %v", warnings)
	}
}

func TestValidateRuntime_CrossCheckPairing(t *testing.T) {
	tests := []struct {
		name             string
		crossCheckEnable bool
		crossCheckAgent  string
		crossCheckModel  string
		summarizerAgent  string
		wantMinErrs      int
		wantErrSubstring string
	}{
		{
			name:             "valid 1:1 pairing",
			crossCheckEnable: true,
			crossCheckAgent:  "codex",
			crossCheckModel:  "gpt-5",
			summarizerAgent:  "codex",
			wantMinErrs:      0,
		},
		{
			name:             "valid multi pairing",
			crossCheckEnable: true,
			crossCheckAgent:  "codex,claude",
			crossCheckModel:  "gpt-5,claude-opus-4-6",
			summarizerAgent:  "codex",
			wantMinErrs:      0,
		},
		{
			name:             "count mismatch",
			crossCheckEnable: true,
			crossCheckAgent:  "codex,claude",
			crossCheckModel:  "gpt-5",
			summarizerAgent:  "codex",
			wantMinErrs:      1,
			wantErrSubstring: "same count",
		},
		{
			name:             "empty model token",
			crossCheckEnable: true,
			crossCheckAgent:  "codex",
			crossCheckModel:  "gpt-5,,claude-opus",
			summarizerAgent:  "codex",
			wantMinErrs:      1,
			wantErrSubstring: "empty entry",
		},
		{
			name:             "trailing comma",
			crossCheckEnable: true,
			crossCheckAgent:  "codex",
			crossCheckModel:  "gpt-5,",
			summarizerAgent:  "codex",
			wantMinErrs:      1,
			wantErrSubstring: "empty entry",
		},
		{
			name:             "agent fallback to summarizer",
			crossCheckEnable: true,
			crossCheckAgent:  "",
			crossCheckModel:  "gpt-5",
			summarizerAgent:  "codex",
			wantMinErrs:      0,
		},
		{
			name:             "agent fallback count mismatch",
			crossCheckEnable: true,
			crossCheckAgent:  "",
			crossCheckModel:  "gpt-5,claude-opus",
			summarizerAgent:  "codex",
			wantMinErrs:      1,
			wantErrSubstring: "same count",
		},
		{
			name:             "disabled skips all",
			crossCheckEnable: false,
			crossCheckAgent:  "codex,claude",
			crossCheckModel:  "",
			summarizerAgent:  "codex",
			wantMinErrs:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &ResolvedConfig{
				CrossCheckEnabled: tt.crossCheckEnable,
				CrossCheckAgent:   tt.crossCheckAgent,
				CrossCheckModel:   tt.crossCheckModel,
				SummarizerAgent:   tt.summarizerAgent,
				CrossCheckTimeout: 5 * time.Minute,
			}
			errs := rc.ValidateRuntime()
			if len(errs) < tt.wantMinErrs {
				t.Errorf("expected at least %d error(s), got %d: %v", tt.wantMinErrs, len(errs), errs)
			}
			if tt.wantErrSubstring != "" {
				found := false
				for _, e := range errs {
					if strings.Contains(e, tt.wantErrSubstring) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got %v", tt.wantErrSubstring, errs)
				}
			}
			if tt.wantMinErrs == 0 && len(errs) != 0 {
				t.Errorf("expected no errors, got %v", errs)
			}
		})
	}
}

// TestValidateRuntime_CrossCheckModelsTreeFallback exercises the Round-13 F#3
// tightening: ValidateRuntime used to accept ANY cross_check entry in the
// models tree as sufficient coverage, which let partial configs slip through
// validation only to blow up at runtime with "cross-check-model is required".
// The fix is per-agent: every selected cross_check agent must have a
// resolvable path in the tree (agents / sizes / defaults), else validation
// reports which agent is uncovered.
func TestValidateRuntime_CrossCheckModelsTreeFallback(t *testing.T) {
	wantSubstr := "cross_check.enabled=true requires cross_check.model"

	t.Run("agents_tree_covers_all_selected_agents", func(t *testing.T) {
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex,claude",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Agents: map[string]RoleModels{
					"codex":  {CrossCheck: &ModelSpec{Model: "gpt-5.4-mini"}},
					"claude": {CrossCheck: &ModelSpec{Model: "claude-opus-4-7"}},
				},
			},
		}
		errs := r.ValidateRuntime()
		if containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected no cross_check runtime error when every selected agent is covered, got: %v", errs)
		}
	})

	t.Run("defaults_tree_covers_all_selected_agents", func(t *testing.T) {
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex,claude",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Defaults: RoleModels{CrossCheck: &ModelSpec{Model: "shared-cc-model"}},
			},
		}
		errs := r.ValidateRuntime()
		if containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected no cross_check runtime error when defaults tree covers all, got: %v", errs)
		}
	})

	t.Run("sizes_tree_covers_all_selected_agents", func(t *testing.T) {
		// sizes-only config legitimately expects the size layer to activate
		// at runtime. Validate tolerates ANY size having a model because the
		// size is unknown at validate time.
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex,claude",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Sizes: map[string]RoleModels{
					"large": {CrossCheck: &ModelSpec{Model: "gpt-5.4-large"}},
				},
			},
		}
		errs := r.ValidateRuntime()
		if containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected no cross_check runtime error when sizes tree covers all, got: %v", errs)
		}
	})

	t.Run("partial_agents_tree_fails_with_agent_name", func(t *testing.T) {
		// Round-13 F#3 regression guard: one agent covered, the other not.
		// Before fix: hasCrossCheckModelInModelsTree returned true because
		// codex entry existed → validation passed → runtime blew up on claude.
		// After fix: validation names "claude" explicitly.
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex,claude",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Agents: map[string]RoleModels{
					"codex": {CrossCheck: &ModelSpec{Model: "gpt-5.4-mini"}},
				},
			},
		}
		errs := r.ValidateRuntime()
		if !containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected cross_check runtime error when claude is uncovered, got: %v", errs)
		}
		if !containsSubstr(errs, "claude") {
			t.Errorf("error must name the uncovered agent 'claude', got: %v", errs)
		}
	})

	t.Run("effort_only_entries_do_not_satisfy_model_requirement", func(t *testing.T) {
		// Effort alone never satisfies the runtime Model requirement.
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Agents: map[string]RoleModels{
					"codex": {CrossCheck: &ModelSpec{Effort: "high"}}, // no Model
				},
			},
		}
		errs := r.ValidateRuntime()
		if !containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected cross_check runtime error when only Effort is set, got: %v", errs)
		}
	})

	// Round-14 F#1 (案 V): cross-check runs exclusively at size=large, so the
	// validate-time tolerance for "ANY size" is tightened to "sizes.large only".
	// sizes.small / sizes.medium entries are dead config for the cross_check
	// role (runtime modelconfig.Resolve receives sizeStr="large" at the gate),
	// and the validate layer now rejects them with an actionable error.
	t.Run("sizes_tree_only_covers_small_now_rejected", func(t *testing.T) {
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Sizes: map[string]RoleModels{
					"small": {CrossCheck: &ModelSpec{Model: "gpt-5.4-small"}},
				},
			},
		}
		errs := r.ValidateRuntime()
		if !containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected cross_check runtime error when only sizes.small is set, got: %v", errs)
		}
		if !containsSubstr(errs, "sizes.large") {
			t.Errorf("error must guide users to sizes.large, got: %v", errs)
		}
	})

	t.Run("sizes_tree_only_covers_medium_now_rejected", func(t *testing.T) {
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Sizes: map[string]RoleModels{
					"medium": {CrossCheck: &ModelSpec{Model: "gpt-5.4-medium"}},
				},
			},
		}
		errs := r.ValidateRuntime()
		if !containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected cross_check runtime error when only sizes.medium is set, got: %v", errs)
		}
		if !containsSubstr(errs, "sizes.large") {
			t.Errorf("error must guide users to sizes.large, got: %v", errs)
		}
	})

	t.Run("sizes_large_alone_still_accepted", func(t *testing.T) {
		// Round-14 F#1 regression guard: tightening sizes layer to "large only"
		// must not break users who configure cross_check under sizes.large.
		r := &ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			CrossCheckTimeout: 5 * time.Minute,
			Models: ModelsConfig{
				Sizes: map[string]RoleModels{
					"large": {CrossCheck: &ModelSpec{Model: "gpt-5.4-large"}},
				},
			},
		}
		errs := r.ValidateRuntime()
		if containsSubstr(errs, wantSubstr) {
			t.Fatalf("expected sizes.large alone to still satisfy validate, got: %v", errs)
		}
	})
}

// --- RolePrompts config resolution tests ---

func TestResolve_RolePromptsDefaultsTrue(t *testing.T) {
	result := Resolve(&Config{}, EnvState{}, FlagState{}, ResolvedConfig{})
	if !result.RolePrompts {
		t.Errorf("expected RolePrompts=true as default, got false")
	}
}

func TestResolve_RolePromptsFromYAML(t *testing.T) {
	cfg := &Config{RolePrompts: boolPtr(true)}
	result := Resolve(cfg, EnvState{}, FlagState{}, ResolvedConfig{})
	if !result.RolePrompts {
		t.Errorf("expected RolePrompts=true from yaml, got false")
	}
}

func TestResolve_RolePromptsFromEnv(t *testing.T) {
	t.Setenv("ACR_ROLE_PROMPTS", "true")
	env, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	result := Resolve(&Config{}, env, FlagState{}, ResolvedConfig{})
	if !result.RolePrompts {
		t.Errorf("expected RolePrompts=true from env, got false")
	}
}

func TestResolve_RolePromptsFlagOverridesEnv(t *testing.T) {
	env := EnvState{RolePrompts: true, RolePromptsSet: true}
	result := Resolve(&Config{}, env, FlagState{RolePromptsSet: true}, ResolvedConfig{RolePrompts: false})
	if result.RolePrompts {
		t.Errorf("expected flag --no-role-prompts to override env true, got true")
	}
}

func TestResolve_RolePromptsFlagOverridesYAML(t *testing.T) {
	cfg := &Config{RolePrompts: boolPtr(true)}
	result := Resolve(cfg, EnvState{}, FlagState{RolePromptsSet: true}, ResolvedConfig{RolePrompts: false})
	if result.RolePrompts {
		t.Errorf("expected flag --no-role-prompts to override yaml true, got true")
	}

	cfgFalse := &Config{RolePrompts: boolPtr(false)}
	result = Resolve(cfgFalse, EnvState{}, FlagState{RolePromptsSet: true}, ResolvedConfig{RolePrompts: true})
	if !result.RolePrompts {
		t.Errorf("expected flag --role-prompts to override yaml false, got false")
	}
}

// TestResolve_RolePrompts_NeitherFlagSet_EnvTrue verifies that when neither
// --role-prompts nor --no-role-prompts is set (RolePromptsSet=false), the env
// var ACR_ROLE_PROMPTS=true is respected. This guards against the bug where
// main.go collapsed both flags into a single boolean that defaulted to false,
// causing RolePromptsSet=true + RolePrompts=false even when no flag was passed.
func TestResolve_RolePrompts_NeitherFlagSet_EnvTrue(t *testing.T) {
	env := EnvState{RolePrompts: true, RolePromptsSet: true}
	// flagValues.RolePrompts=false simulates the default that main.go sets
	// when neither --role-prompts nor --no-role-prompts is Changed.
	flagValues := ResolvedConfig{RolePrompts: false}
	// Neither flag is Changed → RolePromptsSet must be false.
	flagState := FlagState{RolePromptsSet: false}
	result := Resolve(&Config{}, env, flagState, flagValues)
	if !result.RolePrompts {
		t.Errorf("expected RolePrompts=true from env when no flag is set, got false")
	}
}

func TestCheckUnknownKeys_RolePromptsIsKnown(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := "role_prompts: true\n"
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range result.Warnings {
		if strings.Contains(w, "role_prompts") {
			t.Errorf("role_prompts should be a known key, got warning: %s", w)
		}
	}
}
