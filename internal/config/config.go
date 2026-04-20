// Package config provides configuration file support for acr.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// ConfigFileName is the name of the config file.
const ConfigFileName = ".acr.yaml"

// Duration is a custom type that handles YAML duration parsing.
// Supports both Go duration format ("5m", "300s") and numeric seconds.
type Duration time.Duration

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw interface{}
	if err := unmarshal(&raw); err != nil {
		return err
	}

	switch v := raw.(type) {
	case string:
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", v, err)
		}
		*d = Duration(parsed)
	case int:
		*d = Duration(time.Duration(v) * time.Second)
	case float64:
		*d = Duration(time.Duration(v) * time.Second)
	default:
		return fmt.Errorf("invalid duration type: %T", v)
	}
	return nil
}

// Duration returns the underlying time.Duration.
func (d Duration) AsDuration() time.Duration {
	return time.Duration(d)
}

// ModelSpec holds the model name and effort for a specific role.
type ModelSpec struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

// RoleModels holds per-role model specifications.
//
// ArchReviewer / DiffReviewer are phase-specific reviewer overrides used only
// by auto-phase medium/large runs where `arch` and `diff` phases run with
// distinct model/effort. When unset, the resolver falls back to the generic
// Reviewer spec at the SAME cascade layer (see modelconfig.ResolveReviewer).
// Flat review (auto-phase OFF / size=small / explicit `--phase diff`) ignores
// these fields and uses Reviewer only.
type RoleModels struct {
	Reviewer     *ModelSpec `yaml:"reviewer"`
	ArchReviewer *ModelSpec `yaml:"arch_reviewer"`
	DiffReviewer *ModelSpec `yaml:"diff_reviewer"`
	Summarizer   *ModelSpec `yaml:"summarizer"`
	FPFilter     *ModelSpec `yaml:"fp_filter"`
	CrossCheck   *ModelSpec `yaml:"cross_check"`
	PRFeedback   *ModelSpec `yaml:"pr_feedback"`
}

// ModelsConfig holds the models configuration section.
type ModelsConfig struct {
	Defaults RoleModels            `yaml:"defaults"`
	Sizes    map[string]RoleModels `yaml:"sizes"`
	Agents   map[string]RoleModels `yaml:"agents"`
}

type Config struct {
	Reviewers      *int      `yaml:"reviewers"`
	Concurrency    *int      `yaml:"concurrency"`
	Base           *string   `yaml:"base"`
	Timeout        *Duration `yaml:"timeout"`
	Retries        *int      `yaml:"retries"`
	Fetch          *bool     `yaml:"fetch"`
	ReviewerAgent  *string   `yaml:"reviewer_agent"`
	ReviewerAgents []string  `yaml:"reviewer_agents"`
	// ArchReviewerAgent is the optional per-phase override for the arch phase
	// in auto-phase grouped diff. When unset (nil), it falls back to the first
	// entry of ReviewerAgents.
	ArchReviewerAgent *string `yaml:"arch_reviewer_agent"`
	// DiffReviewerAgents is the optional per-phase override for the diff phase
	// in auto-phase grouped diff (round-robin). When empty, it falls back to
	// ReviewerAgents.
	DiffReviewerAgents []string         `yaml:"diff_reviewer_agents"`
	SummarizerAgent    *string          `yaml:"summarizer_agent"`
	ReviewerModel      *string          `yaml:"reviewer_model"`
	SummarizerModel    *string          `yaml:"summarizer_model"`
	SummarizerTimeout  *Duration        `yaml:"summarizer_timeout"`
	FPFilterTimeout    *Duration        `yaml:"fp_filter_timeout"`
	CrossCheckTimeout  *Duration        `yaml:"cross_check_timeout"`
	GuidanceFile       *string          `yaml:"guidance_file"`
	AutoPhase          *bool            `yaml:"auto_phase"`
	Filters            FilterConfig     `yaml:"filters"`
	FPFilter           FPFilterConfig   `yaml:"fp_filter"`
	PRFeedback         PRFeedbackConfig `yaml:"pr_feedback"`
	CrossCheck         CrossCheckConfig `yaml:"cross_check"`
	Models             ModelsConfig     `yaml:"models"`
}

// CrossCheckConfig holds cross-check verification settings.
type CrossCheckConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Agent   *string `yaml:"agent"`
	Model   *string `yaml:"model"`
}

type FPFilterConfig struct {
	Enabled   *bool `yaml:"enabled"`
	Threshold *int  `yaml:"threshold"`
}

// PRFeedbackConfig holds PR feedback summarization settings.
type PRFeedbackConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Agent   *string `yaml:"agent"`
}

// FilterConfig holds filter-related configuration.
type FilterConfig struct {
	ExcludePatterns []string `yaml:"exclude_patterns"`
}

// LoadWithWarnings reads .acr.yaml from the git repository root and returns warnings.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadWithWarnings() (*LoadResult, error) {
	repoRoot, err := git.GetRoot()
	if err != nil {
		// Not in a git repo - return empty config
		return &LoadResult{Config: &Config{}}, nil
	}

	configPath := filepath.Join(repoRoot, ConfigFileName)
	return LoadFromPathWithWarnings(configPath)
}

// LoadFromDirWithWarnings reads .acr.yaml from the specified directory and returns warnings.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadFromDirWithWarnings(dir string) (*LoadResult, error) {
	configPath := filepath.Join(dir, ConfigFileName)
	return LoadFromPathWithWarnings(configPath)
}

// LoadResult contains the loaded config and any warnings encountered.
type LoadResult struct {
	Config    *Config
	ConfigDir string // Directory containing the config file (for resolving relative paths)
	Warnings  []string
}

// LoadFromPathWithWarnings reads a config file and returns warnings for unknown keys.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadFromPathWithWarnings(path string) (*LoadResult, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &LoadResult{Config: &Config{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Check for unknown keys using strict mode
	warnings := checkUnknownKeys(data)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", ConfigFileName, err)
	}

	// Validate regex patterns
	if err := cfg.validatePatterns(); err != nil {
		return nil, err
	}

	// Check for deprecated fields before validation so warnings are reported
	// even when the config has semantic errors
	if cfg.ReviewerAgent != nil {
		warnings = append(warnings, `"reviewer_agent" is deprecated, use "reviewer_agents" list instead`)
		if len(cfg.ReviewerAgents) > 0 {
			warnings = append(warnings, `both "reviewer_agent" and "reviewer_agents" are set; "reviewer_agents" takes precedence`)
		}
	}

	// Validate config values (return result with warnings even on error so callers
	// can access the parsed config and unknown-key warnings)
	if err := cfg.Validate(); err != nil {
		return &LoadResult{Config: &cfg, ConfigDir: filepath.Dir(path), Warnings: warnings}, fmt.Errorf("%s: %w", ConfigFileName, err)
	}

	return &LoadResult{Config: &cfg, ConfigDir: filepath.Dir(path), Warnings: warnings}, nil
}

// validatePatterns checks that all exclude patterns are valid regex.
func (c *Config) validatePatterns() error {
	for _, pattern := range c.Filters.ExcludePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid regex pattern %q in %s: %w", pattern, ConfigFileName, err)
		}
	}
	return nil
}

var knownTopLevelKeys = []string{"reviewers", "concurrency", "base", "timeout", "retries", "fetch", "reviewer_agent", "reviewer_agents", "arch_reviewer_agent", "diff_reviewer_agents", "summarizer_agent", "reviewer_model", "summarizer_model", "summarizer_timeout", "fp_filter_timeout", "cross_check_timeout", "guidance_file", "auto_phase", "filters", "fp_filter", "pr_feedback", "cross_check", "models"}

var knownFPFilterKeys = []string{"enabled", "threshold"}

var knownModelsKeys = []string{"defaults", "sizes", "agents"}

var knownRoleKeys = []string{"reviewer", "arch_reviewer", "diff_reviewer", "summarizer", "fp_filter", "cross_check", "pr_feedback"}

var knownModelSpecKeys = []string{"model", "effort"}

var knownPRFeedbackKeys = []string{"enabled", "agent"}

var knownCrossCheckKeys = []string{"enabled", "agent", "model"}

// knownFilterKeys are the valid keys under the "filters" section.
var knownFilterKeys = []string{"exclude_patterns"}

// checkUnknownKeys checks for unknown keys in the YAML data and returns warnings.
func checkUnknownKeys(data []byte) []string {
	var warnings []string

	// Parse into a generic map to inspect keys
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// If we can't parse, let the main parser handle the error
		return nil
	}

	// Check top-level keys
	for key := range raw {
		if !slices.Contains(knownTopLevelKeys, key) {
			warning := fmt.Sprintf("unknown key %q in %s", key, ConfigFileName)
			if suggestion := findSimilar(key, knownTopLevelKeys); suggestion != "" {
				warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
			}
			warnings = append(warnings, warning)
		}
	}

	if filters, ok := raw["filters"].(map[string]any); ok {
		for key := range filters {
			if !slices.Contains(knownFilterKeys, key) {
				warning := fmt.Sprintf("unknown key %q in filters section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownFilterKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	if fpFilter, ok := raw["fp_filter"].(map[string]any); ok {
		for key := range fpFilter {
			if !slices.Contains(knownFPFilterKeys, key) {
				warning := fmt.Sprintf("unknown key %q in fp_filter section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownFPFilterKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	if prFeedback, ok := raw["pr_feedback"].(map[string]any); ok {
		for key := range prFeedback {
			if !slices.Contains(knownPRFeedbackKeys, key) {
				warning := fmt.Sprintf("unknown key %q in pr_feedback section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownPRFeedbackKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	if crossCheck, ok := raw["cross_check"].(map[string]any); ok {
		for key := range crossCheck {
			if !slices.Contains(knownCrossCheckKeys, key) {
				warning := fmt.Sprintf("unknown key %q in cross_check section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownCrossCheckKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	if models, ok := raw["models"].(map[string]any); ok {
		for key := range models {
			if !slices.Contains(knownModelsKeys, key) {
				warning := fmt.Sprintf("unknown key %q in models section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownModelsKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}

		// Check defaults section role keys and modelspec keys
		if defaults, ok := models["defaults"].(map[string]any); ok {
			warnings = append(warnings, checkRoleModelsKeys(defaults, "models.defaults", ConfigFileName)...)
		}

		// Check sizes: each size name is user-defined, but its role keys must be known
		if sizes, ok := models["sizes"].(map[string]any); ok {
			for sizeName, sizeVal := range sizes {
				if roleMap, ok := sizeVal.(map[string]any); ok {
					warnings = append(warnings, checkRoleModelsKeys(roleMap, fmt.Sprintf("models.sizes.%s", sizeName), ConfigFileName)...)
				}
			}
		}

		// Check agents: each agent name is validated in Validate(), but role keys must be known
		if agents, ok := models["agents"].(map[string]any); ok {
			for agentName, agentVal := range agents {
				if roleMap, ok := agentVal.(map[string]any); ok {
					warnings = append(warnings, checkRoleModelsKeys(roleMap, fmt.Sprintf("models.agents.%s", agentName), ConfigFileName)...)
				}
			}
		}
	}

	return warnings
}

// checkRoleModelsKeys checks that all keys in a RoleModels map are known role keys,
// and that all keys within each ModelSpec are known modelspec keys.
func checkRoleModelsKeys(roleMap map[string]any, section, configFileName string) []string {
	var warnings []string
	for roleKey, roleVal := range roleMap {
		if !slices.Contains(knownRoleKeys, roleKey) {
			warning := fmt.Sprintf("unknown key %q in %s section of %s", roleKey, section, configFileName)
			if suggestion := findSimilar(roleKey, knownRoleKeys); suggestion != "" {
				warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
			}
			warnings = append(warnings, warning)
			continue
		}
		if specMap, ok := roleVal.(map[string]any); ok {
			for specKey := range specMap {
				if !slices.Contains(knownModelSpecKeys, specKey) {
					warning := fmt.Sprintf("unknown key %q in %s.%s section of %s", specKey, section, roleKey, configFileName)
					if suggestion := findSimilar(specKey, knownModelSpecKeys); suggestion != "" {
						warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
					}
					warnings = append(warnings, warning)
				}
			}
		}
	}
	return warnings
}

// findSimilar finds the most similar string from candidates using Levenshtein distance.
// Returns empty string if no candidate is similar enough (threshold: 3 edits).
func findSimilar(input string, candidates []string) string {
	const maxDistance = 3
	bestMatch := ""
	bestDistance := maxDistance + 1

	for _, candidate := range candidates {
		dist := levenshtein(input, candidate)
		if dist < bestDistance {
			bestDistance = dist
			bestMatch = candidate
		}
	}

	if bestDistance <= maxDistance {
		return bestMatch
	}
	return ""
}

// levenshtein calculates the Levenshtein distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)

	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	// Create matrix
	matrix := make([][]int, len(ra)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(rb)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(ra)][len(rb)]
}

// parseCommaSeparated splits a comma-separated string into a slice of trimmed strings.
// Returns nil if no non-empty parts are found, so callers can distinguish
// "not set" from "set but empty".
func parseCommaSeparated(input string) []string {
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// Merge combines config file patterns with CLI patterns.
// CLI patterns are appended after config patterns (both are applied).
func Merge(cfg *Config, cliPatterns []string) []string {
	if cfg == nil {
		return cliPatterns
	}
	return append(cfg.Filters.ExcludePatterns, cliPatterns...)
}

// Validate checks that all config file values are semantically valid.
// Delegates to ResolvedConfig.ValidateAll() by resolving config-only values against defaults,
// so validation rules are defined in one place.
func (c *Config) Validate() error {
	resolved := Resolve(c, EnvState{}, FlagState{}, Defaults)
	errs := resolved.ValidateAll()
	errs = append(errs, c.validateModelsAgents()...)
	errs = append(errs, c.validateModelsSizes()...)
	errs = append(errs, c.validateModelsEffort()...)
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// ValidateAll checks that all resolved config values are semantically valid.
// Returns individual error strings so callers can count and report them accurately.
func (r *ResolvedConfig) ValidateAll() []string {
	var errs []string
	if r.Reviewers < 1 {
		errs = append(errs, fmt.Sprintf("reviewers must be >= 1, got %d", r.Reviewers))
	}
	if r.Concurrency < 0 {
		errs = append(errs, fmt.Sprintf("concurrency must be >= 0, got %d", r.Concurrency))
	}
	if r.Retries < 0 {
		errs = append(errs, fmt.Sprintf("retries must be >= 0, got %d", r.Retries))
	}
	if r.Timeout <= 0 {
		errs = append(errs, fmt.Sprintf("timeout must be > 0, got %s", r.Timeout))
	}
	if r.SummarizerTimeout <= 0 {
		errs = append(errs, fmt.Sprintf("summarizer_timeout must be > 0, got %s", r.SummarizerTimeout))
	}
	if r.FPFilterTimeout <= 0 {
		errs = append(errs, fmt.Sprintf("fp_filter_timeout must be > 0, got %s", r.FPFilterTimeout))
	}
	if len(r.ReviewerAgents) == 0 {
		errs = append(errs, "reviewer_agents must not be empty")
	}
	for _, a := range r.ReviewerAgents {
		if !slices.Contains(agent.SupportedAgents, a) {
			errs = append(errs, fmt.Sprintf("reviewer_agents contains unsupported agent %q, must be one of %v", a, agent.SupportedAgents))
		}
	}
	// arch_reviewer_agent is optional; empty string means fall back to
	// ReviewerAgents[0]. When set, it must be a supported agent.
	if r.ArchReviewerAgent != "" && !slices.Contains(agent.SupportedAgents, r.ArchReviewerAgent) {
		errs = append(errs, fmt.Sprintf("arch_reviewer_agent must be one of %v, got %q", agent.SupportedAgents, r.ArchReviewerAgent))
	}
	// diff_reviewer_agents is optional; empty list means fall back to
	// ReviewerAgents. When set, every entry must be a supported agent.
	for _, a := range r.DiffReviewerAgents {
		if !slices.Contains(agent.SupportedAgents, a) {
			errs = append(errs, fmt.Sprintf("diff_reviewer_agents contains unsupported agent %q, must be one of %v", a, agent.SupportedAgents))
		}
	}
	if !slices.Contains(agent.SupportedAgents, r.SummarizerAgent) {
		errs = append(errs, fmt.Sprintf("summarizer_agent must be one of %v, got %q", agent.SupportedAgents, r.SummarizerAgent))
	}
	if r.FPThreshold < 1 || r.FPThreshold > 100 {
		errs = append(errs, fmt.Sprintf("fp_filter.threshold must be 1-100, got %d", r.FPThreshold))
	}
	if r.PRFeedbackAgent != "" && !slices.Contains(agent.SupportedAgents, r.PRFeedbackAgent) {
		errs = append(errs, fmt.Sprintf("pr_feedback.agent must be one of %v, got %q", agent.SupportedAgents, r.PRFeedbackAgent))
	}
	if r.CrossCheckAgent != "" {
		for _, tok := range strings.Split(r.CrossCheckAgent, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				errs = append(errs, "cross_check.agent contains an empty token; check for trailing commas or whitespace-only entries")
				continue
			}
			if !slices.Contains(agent.SupportedAgents, tok) {
				errs = append(errs, fmt.Sprintf("cross_check.agent contains unsupported agent %q, must be one of %v", tok, agent.SupportedAgents))
			}
		}
	}
	if r.CrossCheckTimeout <= 0 {
		errs = append(errs, fmt.Sprintf("cross_check_timeout must be > 0, got %s", r.CrossCheckTimeout))
	}
	return errs
}

// ValidateRuntime returns errors that should only be enforced once all
// configuration sources (config file + env + CLI flags) have been merged into
// the final ResolvedConfig. Unlike ValidateAll (which is also invoked during
// YAML parse, where only the file + Defaults are visible), these checks would
// produce false positives for users who legitimately supply the value through
// env or CLI. main.go calls this immediately after Resolve(); config validate
// invokes it as well so users get the same fail-fast behavior interactively.
func (r *ResolvedConfig) ValidateRuntime() []string {
	var errs []string
	// Round-9 contract: when cross-check is enabled (the default; disabling it
	// also disables auto-phase grouped review consistency), the user MUST pick
	// a model. Defaults intentionally leaves CrossCheckModel empty so this
	// guard fires for users who never configured cross-check at all.
	if r.CrossCheckEnabled && strings.TrimSpace(r.CrossCheckModel) == "" {
		errs = append(errs, "cross_check.enabled=true requires cross_check.model (or --cross-check-model / ACR_CROSS_CHECK_MODEL); supply a comma-separated model list paired 1:1 with cross_check.agent, or disable cross-check explicitly with cross_check.enabled=false / --no-cross-check (note: disabling cross-check forfeits auto-phase grouped review consistency)")
	}
	return errs
}

// validateModelsAgents checks that all agent keys in models.agents are supported agents.
func (c *Config) validateModelsAgents() []string {
	var errs []string
	for agentName := range c.Models.Agents {
		if !slices.Contains(agent.SupportedAgents, agentName) {
			errs = append(errs, fmt.Sprintf("models.agents contains unsupported agent %q, must be one of %v", agentName, agent.SupportedAgents))
		}
	}
	return errs
}

var validSizeKeys = []string{"small", "medium", "large"}

// validateModelsSizes checks that all size keys in models.sizes are valid.
func (c *Config) validateModelsSizes() []string {
	var errs []string
	for sizeName := range c.Models.Sizes {
		if !slices.Contains(validSizeKeys, sizeName) {
			errs = append(errs, fmt.Sprintf(
				"models.sizes contains unknown size key %q, must be one of %v",
				sizeName, validSizeKeys,
			))
		}
	}
	return errs
}

// validEffortsLoose is the superset accepted in defaults/sizes layers
// (not agent-specific; uses the broadest supported set).
var validEffortsLoose = []string{"low", "medium", "high", "xhigh", "max"}

// validEffortsByAgent is the per-agent accepted set for the agents layer.
var validEffortsByAgent = map[string][]string{
	"codex":  {"low", "medium", "high"},
	"claude": {"low", "medium", "high", "xhigh", "max"},
	"gemini": {},
}

// validateModelsEffort validates effort values across defaults, sizes, and agents layers.
func (c *Config) validateModelsEffort() []string {
	var errs []string

	checkAllSpecEfforts(c.Models.Defaults, "defaults", validEffortsLoose, &errs)

	for sizeName, roleModels := range c.Models.Sizes {
		checkAllSpecEfforts(roleModels, "sizes."+sizeName, validEffortsLoose, &errs)
	}

	for agentName, roleModels := range c.Models.Agents {
		valid, ok := validEffortsByAgent[agentName]
		if !ok {
			// Unknown agent — validateModelsAgents already catches this, skip.
			continue
		}
		checkAllSpecEfforts(roleModels, "agents."+agentName, valid, &errs)
	}

	return errs
}

// checkAllSpecEfforts validates all ModelSpec.Effort fields in a RoleModels struct.
func checkAllSpecEfforts(rm RoleModels, prefix string, valid []string, errs *[]string) {
	checkOne := func(spec *ModelSpec, role string) {
		if spec == nil || spec.Effort == "" {
			return
		}
		if !slices.Contains(valid, strings.ToLower(spec.Effort)) {
			*errs = append(*errs, fmt.Sprintf(
				"models.%s.%s.effort %q is not valid (accepted: %v)",
				prefix, role, spec.Effort, valid,
			))
		}
	}
	checkOne(rm.Reviewer, "reviewer")
	checkOne(rm.ArchReviewer, "arch_reviewer")
	checkOne(rm.DiffReviewer, "diff_reviewer")
	checkOne(rm.Summarizer, "summarizer")
	checkOne(rm.FPFilter, "fp_filter")
	checkOne(rm.CrossCheck, "cross_check")
	checkOne(rm.PRFeedback, "pr_feedback")
}

// Validate checks that all resolved config values are semantically valid.
// Returns a single error summarizing all issues, or nil if valid.
func (r *ResolvedConfig) Validate() error {
	errs := r.ValidateAll()
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid resolved configuration:\n  - %s", strings.Join(errs, "\n  - "))
}

var Defaults = ResolvedConfig{
	Reviewers:         5,
	Concurrency:       0,
	Base:              "main",
	Timeout:           10 * time.Minute,
	Retries:           1,
	Fetch:             true,
	ReviewerAgents:    []string{agent.DefaultAgent},
	SummarizerAgent:   agent.DefaultSummarizerAgent,
	SummarizerTimeout: 5 * time.Minute,
	FPFilterTimeout:   5 * time.Minute,
	CrossCheckTimeout: 5 * time.Minute,
	FPFilterEnabled:   true,
	FPThreshold:       75,
	PRFeedbackEnabled: true,
	PRFeedbackAgent:   "", // empty means use summarizer agent
	CrossCheckEnabled: true,
	CrossCheckAgent:   "", // empty means use summarizer agent
	CrossCheckModel:   "", // empty means use summarizer model
	AutoPhase:         true,
	Strict:            false,
}

type ResolvedConfig struct {
	Reviewers      int
	Concurrency    int
	Base           string
	Timeout        time.Duration
	Retries        int
	Fetch          bool
	ReviewerAgents []string
	// ArchReviewerAgent is the per-phase override for the arch phase in
	// auto-phase grouped diff. Empty string means fall back to the first
	// entry of ReviewerAgents.
	ArchReviewerAgent string
	// DiffReviewerAgents is the per-phase override for the diff phase in
	// auto-phase grouped diff (round-robin). Empty slice means fall back to
	// ReviewerAgents.
	DiffReviewerAgents []string
	SummarizerAgent    string
	ReviewerModel      string
	SummarizerModel    string
	SummarizerTimeout  time.Duration
	FPFilterTimeout    time.Duration
	CrossCheckTimeout  time.Duration
	Guidance           string
	GuidanceFile       string
	FPFilterEnabled    bool
	FPThreshold        int
	PRFeedbackEnabled  bool
	PRFeedbackAgent    string
	CrossCheckEnabled  bool
	CrossCheckAgent    string // empty means use summarizer agent
	CrossCheckModel    string // empty means use summarizer model
	AutoPhase          bool
	Strict             bool // when true, advisory verdict exits 1 (default false)
	// ReviewerModelFromCLI is true when --reviewer-model or ACR_REVIEWER_MODEL
	// set the ReviewerModel field, making it a CLI/env override that should win
	// over models.agents/sizes/defaults config.
	ReviewerModelFromCLI   bool
	SummarizerModelFromCLI bool
	CrossCheckModelFromCLI bool
	Models                 ModelsConfig
}

type FlagState struct {
	ReviewersSet          bool
	ConcurrencySet        bool
	BaseSet               bool
	TimeoutSet            bool
	RetriesSet            bool
	FetchSet              bool
	ReviewerAgentsSet     bool
	ArchReviewerAgentSet  bool
	DiffReviewerAgentsSet bool
	SummarizerAgentSet    bool
	ReviewerModelSet      bool
	SummarizerModelSet    bool
	SummarizerTimeoutSet  bool
	FPFilterTimeoutSet    bool
	CrossCheckTimeoutSet  bool
	GuidanceSet           bool
	GuidanceFileSet       bool
	NoFPFilterSet         bool
	FPThresholdSet        bool
	NoPRFeedbackSet       bool
	PRFeedbackAgentSet    bool
	NoCrossCheckSet       bool
	CrossCheckAgentSet    bool
	CrossCheckModelSet    bool
	AutoPhaseSet          bool
	StrictSet             bool
}

type EnvState struct {
	Reviewers             int
	ReviewersSet          bool
	Concurrency           int
	ConcurrencySet        bool
	Base                  string
	BaseSet               bool
	Timeout               time.Duration
	TimeoutSet            bool
	Retries               int
	RetriesSet            bool
	Fetch                 bool
	FetchSet              bool
	ReviewerAgents        []string
	ReviewerAgentsSet     bool
	ArchReviewerAgent     string
	ArchReviewerAgentSet  bool
	DiffReviewerAgents    []string
	DiffReviewerAgentsSet bool
	SummarizerAgent       string
	SummarizerAgentSet    bool
	ReviewerModel         string
	ReviewerModelSet      bool
	SummarizerModel       string
	SummarizerModelSet    bool
	SummarizerTimeout     time.Duration
	SummarizerTimeoutSet  bool
	FPFilterTimeout       time.Duration
	FPFilterTimeoutSet    bool
	Guidance              string
	GuidanceSet           bool
	GuidanceFile          string
	GuidanceFileSet       bool
	FPFilterEnabled       bool
	FPFilterSet           bool
	FPThreshold           int
	FPThresholdSet        bool
	PRFeedbackEnabled     bool
	PRFeedbackEnabledSet  bool
	PRFeedbackAgent       string
	PRFeedbackAgentSet    bool
	CrossCheckEnabled     bool
	CrossCheckEnabledSet  bool
	CrossCheckAgent       string
	CrossCheckAgentSet    bool
	CrossCheckModel       string
	CrossCheckModelSet    bool
	CrossCheckTimeout     time.Duration
	CrossCheckTimeoutSet  bool
	AutoPhase             bool
	AutoPhaseSet          bool
	Strict                bool
	StrictSet             bool
}

// LoadEnvState reads environment variables and returns their state.
// Returns warnings for any environment variables that are set but have invalid values.
func LoadEnvState() (EnvState, []string) {
	var state EnvState
	var warnings []string

	if v := os.Getenv("ACR_REVIEWERS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Reviewers = i
			state.ReviewersSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_REVIEWERS=%q is not a valid integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_CONCURRENCY"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Concurrency = i
			state.ConcurrencySet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_CONCURRENCY=%q is not a valid integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_BASE_REF"); v != "" {
		state.Base = v
		state.BaseSet = true
	}
	if v := os.Getenv("ACR_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.Timeout = d
			state.TimeoutSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.Timeout = time.Duration(secs) * time.Second
			state.TimeoutSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_TIMEOUT=%q is not a valid duration or integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_RETRIES"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Retries = i
			state.RetriesSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_RETRIES=%q is not a valid integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_FETCH"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			state.Fetch = true
			state.FetchSet = true
		case "false", "0", "no":
			state.Fetch = false
			state.FetchSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_FETCH=%q is not a valid boolean (use true/false/1/0/yes/no), ignoring", v))
		}
	}
	if v := os.Getenv("ACR_REVIEWER_AGENT"); v != "" {
		if agents := parseCommaSeparated(v); agents != nil {
			state.ReviewerAgents = agents
			state.ReviewerAgentsSet = true
		}
	}
	if v := os.Getenv("ACR_ARCH_REVIEWER_AGENT"); v != "" {
		state.ArchReviewerAgent = strings.TrimSpace(v)
		state.ArchReviewerAgentSet = true
	}
	if v := os.Getenv("ACR_DIFF_REVIEWER_AGENTS"); v != "" {
		if agents := parseCommaSeparated(v); agents != nil {
			state.DiffReviewerAgents = agents
			state.DiffReviewerAgentsSet = true
		}
	}
	if v := os.Getenv("ACR_SUMMARIZER_AGENT"); v != "" {
		state.SummarizerAgent = v
		state.SummarizerAgentSet = true
	}
	if v := os.Getenv("ACR_REVIEWER_MODEL"); v != "" {
		state.ReviewerModel = v
		state.ReviewerModelSet = true
	}
	if v := os.Getenv("ACR_SUMMARIZER_MODEL"); v != "" {
		state.SummarizerModel = v
		state.SummarizerModelSet = true
	}
	if v := os.Getenv("ACR_SUMMARIZER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.SummarizerTimeout = d
			state.SummarizerTimeoutSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.SummarizerTimeout = time.Duration(secs) * time.Second
			state.SummarizerTimeoutSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_SUMMARIZER_TIMEOUT=%q is not a valid duration or integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_FP_FILTER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.FPFilterTimeout = d
			state.FPFilterTimeoutSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.FPFilterTimeout = time.Duration(secs) * time.Second
			state.FPFilterTimeoutSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_FP_FILTER_TIMEOUT=%q is not a valid duration or integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_GUIDANCE"); v != "" {
		state.Guidance = v
		state.GuidanceSet = true
	}
	if v := os.Getenv("ACR_GUIDANCE_FILE"); v != "" {
		state.GuidanceFile = v
		state.GuidanceFileSet = true
	}

	if v := os.Getenv("ACR_FP_FILTER"); v != "" {
		switch v {
		case "true", "1":
			state.FPFilterEnabled = true
			state.FPFilterSet = true
		case "false", "0":
			state.FPFilterEnabled = false
			state.FPFilterSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_FP_FILTER=%q is not a valid boolean (use true/false/1/0), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_FP_THRESHOLD"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 1 && i <= 100 {
			state.FPThreshold = i
			state.FPThresholdSet = true
		} else if err != nil {
			warnings = append(warnings, fmt.Sprintf("ACR_FP_THRESHOLD=%q is not a valid integer, ignoring", v))
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_FP_THRESHOLD=%q is out of range (must be 1-100), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_PR_FEEDBACK"); v != "" {
		switch v {
		case "true", "1":
			state.PRFeedbackEnabled = true
			state.PRFeedbackEnabledSet = true
		case "false", "0":
			state.PRFeedbackEnabled = false
			state.PRFeedbackEnabledSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_PR_FEEDBACK=%q is not a valid boolean (use true/false/1/0), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_PR_FEEDBACK_AGENT"); v != "" {
		state.PRFeedbackAgent = v
		state.PRFeedbackAgentSet = true
	}

	if v := os.Getenv("ACR_CROSS_CHECK"); v != "" {
		switch v {
		case "true", "1":
			state.CrossCheckEnabled = true
			state.CrossCheckEnabledSet = true
		case "false", "0":
			state.CrossCheckEnabled = false
			state.CrossCheckEnabledSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_CROSS_CHECK=%q is not a valid boolean (use true/false/1/0), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_CROSS_CHECK_AGENT"); v != "" {
		state.CrossCheckAgent = v
		state.CrossCheckAgentSet = true
	}

	if v := os.Getenv("ACR_CROSS_CHECK_MODEL"); v != "" {
		state.CrossCheckModel = v
		state.CrossCheckModelSet = true
	}

	if v := os.Getenv("ACR_CROSS_CHECK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.CrossCheckTimeout = d
			state.CrossCheckTimeoutSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.CrossCheckTimeout = time.Duration(secs) * time.Second
			state.CrossCheckTimeoutSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_CROSS_CHECK_TIMEOUT=%q is not a valid duration or integer, ignoring", v))
		}
	}

	if v := os.Getenv("ACR_AUTO_PHASE"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			state.AutoPhase = true
			state.AutoPhaseSet = true
		case "false", "0", "no":
			state.AutoPhase = false
			state.AutoPhaseSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_AUTO_PHASE=%q is not a valid boolean (use true/false/1/0/yes/no), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_STRICT"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			state.Strict = true
			state.StrictSet = true
		case "false", "0", "no":
			state.Strict = false
			state.StrictSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_STRICT=%q is not a valid boolean (use true/false/1/0/yes/no), ignoring", v))
		}
	}

	return state, warnings
}

// Resolve merges config file values with env vars and flags.
// Precedence: flags > env vars > config file > defaults
func Resolve(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig) ResolvedConfig {
	result := Defaults

	// Apply config file values (if set)
	if cfg != nil {
		if cfg.Reviewers != nil {
			result.Reviewers = *cfg.Reviewers
		}
		if cfg.Concurrency != nil {
			result.Concurrency = *cfg.Concurrency
		}
		if cfg.Base != nil {
			result.Base = *cfg.Base
		}
		if cfg.Timeout != nil {
			result.Timeout = cfg.Timeout.AsDuration()
		}
		if cfg.Retries != nil {
			result.Retries = *cfg.Retries
		}
		if cfg.Fetch != nil {
			result.Fetch = *cfg.Fetch
		}
		// reviewer_agents array takes precedence over reviewer_agent scalar
		if len(cfg.ReviewerAgents) > 0 {
			result.ReviewerAgents = cfg.ReviewerAgents
		} else if cfg.ReviewerAgent != nil {
			result.ReviewerAgents = []string{*cfg.ReviewerAgent}
		}
		if cfg.ArchReviewerAgent != nil {
			result.ArchReviewerAgent = *cfg.ArchReviewerAgent
		}
		if len(cfg.DiffReviewerAgents) > 0 {
			result.DiffReviewerAgents = cfg.DiffReviewerAgents
		}
		if cfg.SummarizerAgent != nil {
			result.SummarizerAgent = *cfg.SummarizerAgent
		}
		if cfg.ReviewerModel != nil {
			result.ReviewerModel = *cfg.ReviewerModel
		}
		if cfg.SummarizerModel != nil {
			result.SummarizerModel = *cfg.SummarizerModel
		}
		if cfg.SummarizerTimeout != nil {
			result.SummarizerTimeout = cfg.SummarizerTimeout.AsDuration()
		}
		if cfg.FPFilterTimeout != nil {
			result.FPFilterTimeout = cfg.FPFilterTimeout.AsDuration()
		}
		if cfg.FPFilter.Enabled != nil {
			result.FPFilterEnabled = *cfg.FPFilter.Enabled
		}
		if cfg.FPFilter.Threshold != nil {
			result.FPThreshold = *cfg.FPFilter.Threshold
		}
		if cfg.PRFeedback.Enabled != nil {
			result.PRFeedbackEnabled = *cfg.PRFeedback.Enabled
		}
		if cfg.PRFeedback.Agent != nil {
			result.PRFeedbackAgent = *cfg.PRFeedback.Agent
		}
		if cfg.CrossCheckTimeout != nil {
			result.CrossCheckTimeout = cfg.CrossCheckTimeout.AsDuration()
		}
		if cfg.CrossCheck.Enabled != nil {
			result.CrossCheckEnabled = *cfg.CrossCheck.Enabled
		}
		if cfg.CrossCheck.Agent != nil {
			result.CrossCheckAgent = *cfg.CrossCheck.Agent
		}
		if cfg.CrossCheck.Model != nil {
			result.CrossCheckModel = *cfg.CrossCheck.Model
		}
		if cfg.AutoPhase != nil {
			result.AutoPhase = *cfg.AutoPhase
		}
		// Models configuration is a struct (not a pointer); copy as-is.
		result.Models = cfg.Models
	}

	// Apply env var values (if set)
	if envState.ReviewersSet {
		result.Reviewers = envState.Reviewers
	}
	if envState.ConcurrencySet {
		result.Concurrency = envState.Concurrency
	}
	if envState.BaseSet {
		result.Base = envState.Base
	}
	if envState.TimeoutSet {
		result.Timeout = envState.Timeout
	}
	if envState.RetriesSet {
		result.Retries = envState.Retries
	}
	if envState.FetchSet {
		result.Fetch = envState.Fetch
	}
	if envState.ReviewerAgentsSet {
		result.ReviewerAgents = envState.ReviewerAgents
	}
	if envState.ArchReviewerAgentSet {
		result.ArchReviewerAgent = envState.ArchReviewerAgent
	}
	if envState.DiffReviewerAgentsSet {
		result.DiffReviewerAgents = envState.DiffReviewerAgents
	}
	if envState.SummarizerAgentSet {
		result.SummarizerAgent = envState.SummarizerAgent
	}
	if envState.ReviewerModelSet {
		result.ReviewerModel = envState.ReviewerModel
	}
	if envState.SummarizerModelSet {
		result.SummarizerModel = envState.SummarizerModel
	}
	if envState.SummarizerTimeoutSet {
		result.SummarizerTimeout = envState.SummarizerTimeout
	}
	if envState.FPFilterTimeoutSet {
		result.FPFilterTimeout = envState.FPFilterTimeout
	}
	if envState.FPFilterSet {
		result.FPFilterEnabled = envState.FPFilterEnabled
	}
	if envState.FPThresholdSet {
		result.FPThreshold = envState.FPThreshold
	}
	if envState.PRFeedbackEnabledSet {
		result.PRFeedbackEnabled = envState.PRFeedbackEnabled
	}
	if envState.PRFeedbackAgentSet {
		result.PRFeedbackAgent = envState.PRFeedbackAgent
	}
	if envState.CrossCheckEnabledSet {
		result.CrossCheckEnabled = envState.CrossCheckEnabled
	}
	if envState.CrossCheckAgentSet {
		result.CrossCheckAgent = envState.CrossCheckAgent
	}
	if envState.CrossCheckModelSet {
		result.CrossCheckModel = envState.CrossCheckModel
	}
	if envState.CrossCheckTimeoutSet {
		result.CrossCheckTimeout = envState.CrossCheckTimeout
	}
	if envState.AutoPhaseSet {
		result.AutoPhase = envState.AutoPhase
	}
	if envState.StrictSet {
		result.Strict = envState.Strict
	}

	if flagState.ReviewersSet {
		result.Reviewers = flagValues.Reviewers
	}
	if flagState.ConcurrencySet {
		result.Concurrency = flagValues.Concurrency
	}
	if flagState.BaseSet {
		result.Base = flagValues.Base
	}
	if flagState.TimeoutSet {
		result.Timeout = flagValues.Timeout
	}
	if flagState.RetriesSet {
		result.Retries = flagValues.Retries
	}
	if flagState.FetchSet {
		result.Fetch = flagValues.Fetch
	}
	if flagState.ReviewerAgentsSet {
		result.ReviewerAgents = flagValues.ReviewerAgents
	}
	if flagState.ArchReviewerAgentSet {
		result.ArchReviewerAgent = flagValues.ArchReviewerAgent
	}
	if flagState.DiffReviewerAgentsSet {
		result.DiffReviewerAgents = flagValues.DiffReviewerAgents
	}
	if flagState.SummarizerAgentSet {
		result.SummarizerAgent = flagValues.SummarizerAgent
	}
	if flagState.ReviewerModelSet {
		result.ReviewerModel = flagValues.ReviewerModel
	}
	if flagState.SummarizerModelSet {
		result.SummarizerModel = flagValues.SummarizerModel
	}
	if flagState.SummarizerTimeoutSet {
		result.SummarizerTimeout = flagValues.SummarizerTimeout
	}
	if flagState.FPFilterTimeoutSet {
		result.FPFilterTimeout = flagValues.FPFilterTimeout
	}
	if flagState.NoFPFilterSet {
		result.FPFilterEnabled = flagValues.FPFilterEnabled
	}
	if flagState.FPThresholdSet {
		result.FPThreshold = flagValues.FPThreshold
	}
	if flagState.NoPRFeedbackSet {
		result.PRFeedbackEnabled = flagValues.PRFeedbackEnabled
	}
	if flagState.PRFeedbackAgentSet {
		result.PRFeedbackAgent = flagValues.PRFeedbackAgent
	}
	if flagState.NoCrossCheckSet {
		result.CrossCheckEnabled = flagValues.CrossCheckEnabled
	}
	if flagState.CrossCheckAgentSet {
		result.CrossCheckAgent = flagValues.CrossCheckAgent
	}
	if flagState.CrossCheckModelSet {
		result.CrossCheckModel = flagValues.CrossCheckModel
	}
	if flagState.CrossCheckTimeoutSet {
		result.CrossCheckTimeout = flagValues.CrossCheckTimeout
	}
	if flagState.AutoPhaseSet {
		result.AutoPhase = flagValues.AutoPhase
	}
	if flagState.StrictSet {
		result.Strict = flagValues.Strict
	}

	result.ReviewerModelFromCLI = flagState.ReviewerModelSet || envState.ReviewerModelSet
	result.SummarizerModelFromCLI = flagState.SummarizerModelSet || envState.SummarizerModelSet
	result.CrossCheckModelFromCLI = flagState.CrossCheckModelSet || envState.CrossCheckModelSet

	return result
}

// ResolveGuidance resolves the review guidance with custom precedence logic.
// Guidance is steering context appended to the built-in prompt, not a replacement.
//
// Precedence (highest to lowest):
// 1. --guidance flag
// 2. --guidance-file flag
// 3. ACR_GUIDANCE env var
// 4. ACR_GUIDANCE_FILE env var
// 5. guidance_file config field
// 6. Empty string (no guidance)
func ResolveGuidance(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig, configDir string) (string, error) {
	if flagState.GuidanceSet && flagValues.Guidance != "" {
		return flagValues.Guidance, nil
	}
	if flagState.GuidanceFileSet && flagValues.GuidanceFile != "" {
		content, err := os.ReadFile(flagValues.GuidanceFile)
		if err != nil {
			return "", fmt.Errorf("failed to read guidance file %q: %w", flagValues.GuidanceFile, err)
		}
		return string(content), nil
	}
	if envState.GuidanceSet && envState.Guidance != "" {
		return envState.Guidance, nil
	}
	if envState.GuidanceFileSet && envState.GuidanceFile != "" {
		content, err := os.ReadFile(envState.GuidanceFile)
		if err != nil {
			return "", fmt.Errorf("failed to read guidance file %q: %w", envState.GuidanceFile, err)
		}
		return string(content), nil
	}
	if cfg != nil && cfg.GuidanceFile != nil && *cfg.GuidanceFile != "" {
		guidancePath := *cfg.GuidanceFile
		if !filepath.IsAbs(guidancePath) && configDir != "" {
			guidancePath = filepath.Join(configDir, guidancePath)
		}
		content, err := os.ReadFile(guidancePath)
		if err != nil {
			return "", fmt.Errorf("failed to read guidance file %q: %w", *cfg.GuidanceFile, err)
		}
		return string(content), nil
	}
	return "", nil
}
