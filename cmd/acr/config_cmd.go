package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/masa6161/arc-cli/internal/config"
	"github.com/masa6161/arc-cli/internal/git"
	"github.com/masa6161/arc-cli/internal/terminal"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage acr configuration",
		Long:  "View, initialize, and validate acr configuration files and environment variables.",
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigValidateCmd())

	return cmd
}

func formatModelSpec(ms *config.ModelSpec) string {
	if ms == nil {
		return "(not set)"
	}
	if ms.Model != "" && ms.Effort != "" {
		return fmt.Sprintf("%s (effort: %s)", ms.Model, ms.Effort)
	}
	if ms.Model != "" {
		return ms.Model
	}
	if ms.Effort != "" {
		return fmt.Sprintf("(effort: %s)", ms.Effort)
	}
	return "(not set)"
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display resolved configuration",
		Long:  "Show the fully resolved configuration from defaults, config file, and environment variables.",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := config.LoadWithWarnings()
			if err != nil {
				return fmt.Errorf("config error: %w", err)
			}

			envState, envWarnings := config.LoadEnvState()

			resolved := config.Resolve(result.Config, envState, config.FlagState{}, config.Defaults)

			// Display warnings from config loading
			allWarnings := append(result.Warnings, envWarnings...)
			for _, w := range allWarnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "[W] %s\n", w)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Resolved configuration:")
			fmt.Fprintln(out)

			// General
			fmt.Fprintf(out, "  %-32s %d\n", "reviewers:", resolved.Reviewers)
			if resolved.Concurrency == 0 {
				fmt.Fprintf(out, "  %-32s %s\n", "concurrency:", "0 (auto)")
			} else {
				fmt.Fprintf(out, "  %-32s %d\n", "concurrency:", resolved.Concurrency)
			}
			fmt.Fprintf(out, "  %-32s %s\n", "base:", resolved.Base)
			fmt.Fprintf(out, "  %-32s %s\n", "timeout:", resolved.Timeout)
			fmt.Fprintf(out, "  %-32s %d\n", "retries:", resolved.Retries)
			fmt.Fprintf(out, "  %-32s %t\n", "fetch:", resolved.Fetch)
			fmt.Fprintf(out, "  %-32s %t\n", "auto_phase:", resolved.AutoPhase)
			fmt.Fprintf(out, "  %-32s %t\n", "strict:", resolved.Strict)
			fmt.Fprintln(out)

			// Agents
			fmt.Fprintf(out, "  %-32s %s\n", "reviewer_agents:", strings.Join(resolved.ReviewerAgents, ", "))
			if resolved.ArchReviewerAgent != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "arch_reviewer_agent:", resolved.ArchReviewerAgent)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "arch_reviewer_agent:", "(first of reviewer_agents)")
			}
			if len(resolved.DiffReviewerAgents) > 0 {
				fmt.Fprintf(out, "  %-32s %s\n", "diff_reviewer_agents:", strings.Join(resolved.DiffReviewerAgents, ", "))
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "diff_reviewer_agents:", "(reviewer_agents)")
			}
			fmt.Fprintf(out, "  %-32s %s\n", "summarizer_agent:", resolved.SummarizerAgent)
			if resolved.CodexHome != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "codex_home (env):", resolved.CodexHome)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "codex_home (env):", "(ACR_CODEX_HOME/CODEX_HOME env or USERPROFILE/HOME/.codex)")
			}
			fmt.Fprintln(out)

			// Models
			if resolved.ReviewerModel != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "reviewer_model:", resolved.ReviewerModel)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "reviewer_model:", "(agent default)")
			}
			if resolved.SummarizerModel != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "summarizer_model:", resolved.SummarizerModel)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "summarizer_model:", "(agent default)")
			}
			fmt.Fprintln(out)

			// Phase knobs
			fmt.Fprintf(out, "  %-32s %d\n", "large_diff_reviewers:", resolved.LargeDiffReviewers)
			fmt.Fprintf(out, "  %-32s %d\n", "medium_diff_reviewers:", resolved.MediumDiffReviewers)
			fmt.Fprintf(out, "  %-32s %d\n", "small_diff_reviewers:", resolved.SmallDiffReviewers)
			fmt.Fprintln(out)

			// Timeouts
			fmt.Fprintf(out, "  %-32s %s\n", "summarizer_timeout:", resolved.SummarizerTimeout)
			fmt.Fprintf(out, "  %-32s %s\n", "fp_filter_timeout:", resolved.FPFilterTimeout)
			fmt.Fprintf(out, "  %-32s %s\n", "cross_check_timeout:", resolved.CrossCheckTimeout)
			fmt.Fprintln(out)

			// FP filter
			fmt.Fprintf(out, "  %-32s %t\n", "fp_filter.enabled:", resolved.FPFilterEnabled)
			fmt.Fprintf(out, "  %-32s %t\n", "fp_filter.triage:", resolved.TriageEnabled)
			fmt.Fprintf(out, "  %-32s %t\n", "fp_filter.show_noise:", resolved.ShowNoise)
			fmt.Fprintf(out, "  %-32s %d\n", "fp_filter.threshold:", resolved.FPThreshold)
			if resolved.FPFilterAgent != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "fp_filter.agent:", resolved.FPFilterAgent)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "fp_filter.agent:", "(same as summarizer_agent)")
			}
			if resolved.FPFilterModel != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "fp_filter.model:", resolved.FPFilterModel)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "fp_filter.model:", "(same as summarizer_model)")
			}
			if resolved.FPFilterEffort != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "fp_filter.effort:", resolved.FPFilterEffort)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "fp_filter.effort:", "(same as summarizer)")
			}
			fmt.Fprintln(out)

			// PR feedback
			fmt.Fprintf(out, "  %-32s %t\n", "pr_feedback.enabled:", resolved.PRFeedbackEnabled)
			if resolved.PRFeedbackAgent != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "pr_feedback.agent:", resolved.PRFeedbackAgent)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "pr_feedback.agent:", "(same as summarizer_agent)")
			}
			fmt.Fprintln(out)

			// Cross-check
			fmt.Fprintf(out, "  %-32s %t\n", "cross_check.enabled:", resolved.CrossCheckEnabled)
			if resolved.CrossCheckAgent != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "cross_check.agent:", resolved.CrossCheckAgent)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "cross_check.agent:", "(same as summarizer_agent)")
			}
			if resolved.CrossCheckModel != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "cross_check.model:", resolved.CrossCheckModel)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "cross_check.model:", "(from models config)")
			}
			fmt.Fprintln(out)

			// Guidance
			if resolved.GuidanceFile != "" {
				fmt.Fprintf(out, "  %-32s %s\n", "guidance_file:", resolved.GuidanceFile)
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "guidance_file:", "(not set)")
			}
			fmt.Fprintln(out)

			// Models config
			fmt.Fprintf(out, "  %-32s %s\n", "models.defaults.reviewer:", formatModelSpec(resolved.Models.Defaults.Reviewer))
			fmt.Fprintf(out, "  %-32s %s\n", "models.defaults.arch_reviewer:", formatModelSpec(resolved.Models.Defaults.ArchReviewer))
			fmt.Fprintf(out, "  %-32s %s\n", "models.defaults.diff_reviewer:", formatModelSpec(resolved.Models.Defaults.DiffReviewer))
			fmt.Fprintf(out, "  %-32s %s\n", "models.defaults.summarizer:", formatModelSpec(resolved.Models.Defaults.Summarizer))
			fmt.Fprintf(out, "  %-32s %s\n", "models.defaults.fp_filter:", formatModelSpec(resolved.Models.Defaults.FPFilter))
			fmt.Fprintf(out, "  %-32s %s\n", "models.defaults.cross_check:", formatModelSpec(resolved.Models.Defaults.CrossCheck))
			fmt.Fprintf(out, "  %-32s %s\n", "models.defaults.pr_feedback:", formatModelSpec(resolved.Models.Defaults.PRFeedback))
			if len(resolved.Models.Sizes) > 0 {
				fmt.Fprintf(out, "  %-32s (%d entries)\n", "models.sizes:", len(resolved.Models.Sizes))
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "models.sizes:", "(none)")
			}
			if len(resolved.Models.Agents) > 0 {
				fmt.Fprintf(out, "  %-32s (%d entries)\n", "models.agents:", len(resolved.Models.Agents))
			} else {
				fmt.Fprintf(out, "  %-32s %s\n", "models.agents:", "(none)")
			}

			return nil
		},
	}
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a starter .acr.yaml file",
		Long:  "Create a commented .acr.yaml configuration file in the git repository root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Write to git repo root (same location runtime loading uses)
			repoRoot, err := git.GetRoot()
			if err != nil {
				return fmt.Errorf("not in a git repository: %w", err)
			}
			configPath := filepath.Join(repoRoot, config.ConfigFileName)

			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("%s already exists; remove it first or edit it directly", configPath)
			}

			starter := `# acr configuration file
# See https://github.com/masa6161/arc-cli for documentation.

# Number of parallel reviewers to run (default: 5)
# reviewers: 5

# Maximum concurrent reviewers (default: same as reviewers)
# concurrency: 0

# Base branch for diff comparison (default: main)
# base: main

# Timeout per reviewer, Go duration format (default: 10m)
# timeout: 10m

# Retry failed reviewers N times (default: 1)
# retries: 1

# Fetch latest base ref from origin before diff (default: true)
# fetch: true

# Agent(s) for reviews: codex, claude, gemini
# reviewer_agents:
#   - codex

# Agent for summarization: codex, claude, gemini
# summarizer_agent: codex

# Codex CLI home is not read from .acr.yaml because repository config is shared.
# Set ACR_CODEX_HOME as a Windows User environment variable instead.
# Precedence: ACR_CODEX_HOME > CODEX_HOME > USERPROFILE/HOME/.codex

# Timeout for summarizer phase (default: 5m)
# summarizer_timeout: 5m

# Timeout for false positive filter phase (default: 5m)
# fp_filter_timeout: 5m

# Path to file containing review guidance
# guidance_file: ""

# Filtering configuration
# filters:
#   exclude_patterns:
#     - "pattern to exclude"

# False positive filtering
# fp_filter:
#   enabled: true
#   threshold: 75
#   triage: true  # Enable severity triage (blocking/advisory/noise classification)
#   show_noise: false  # Show noise-level findings (hidden by default)
#   agent: ""    # FP filter/triage agent (default: same as summarizer_agent)
#   model: ""    # FP filter/triage model (default: same as summarizer_model)
#   effort: ""   # FP filter/triage effort (default: same as summarizer)

# PR feedback summarization
# pr_feedback:
#   enabled: true
#   agent: ""

# Cross-check for grouped review consistency (model is REQUIRED when enabled)
# cross_check:
#   enabled: true
#   agent: codex
#   model: gpt-5
`
			if err := os.WriteFile(configPath, []byte(starter), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", configPath, err)
			}

			fmt.Printf("Created %s with default settings (commented out).\n", configPath)
			return nil
		},
	}
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and environment variables",
		Long:  "Load and validate the config file and environment variables, reporting any warnings or errors.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !terminal.IsStdoutTTY() {
				terminal.DisableColors()
			}
			logger := terminal.NewLogger()
			var errors []string
			var warnings []string

			// Load and validate config file (don't early-return so env var issues are also reported).
			//
			// LoadWithWarnings returns two distinct error shapes:
			//   (1) syntax / regex / IO error → (nil, err). cfg is unusable; we fall
			//       back to Defaults for the rest of validation and skip
			//       ValidateRuntime (running it against synthetic defaults would
			//       false-positive on cross_check.model when the user's actual
			//       intent is hidden behind the broken YAML).
			//   (2) semantic error (cfg.Validate failure) → (result, err) where
			//       result.Config has the user's parseable cfg. We DO want to run
			//       ValidateRuntime against cfg+env here so env-only cross_check
			//       issues are not masked by the semantic-error branch (Round-13 F#2).
			//
			// Round-12 had a single `configFileError` flag that conflated both
			// shapes, which made `config validate` silently skip runtime checks in
			// case (2). Round-13 splits the two and keeps ValidateAll's behavior
			// (use empty resolveConfig in case 2 to avoid duplicating cfg's
			// semantic errors that already appear in the lump "config file: ..."
			// line) while restoring ValidateRuntime for case (2).
			cfg := &config.Config{}
			configDir := ""
			syntaxError := false
			result, err := config.LoadWithWarnings()
			if err != nil {
				errors = append(errors, fmt.Sprintf("config file: %v", err))
				if result == nil {
					syntaxError = true
				}
			}
			if result != nil {
				cfg = result.Config
				configDir = result.ConfigDir
				warnings = append(warnings, result.Warnings...)
			}

			// Check env vars for parse issues. At runtime these are warnings (values are
			// ignored and defaults used), but in validation mode we report them as errors
			// since the user should fix their environment configuration.
			envState, envWarnings := config.LoadEnvState()
			for _, w := range envWarnings {
				if !strings.Contains(w, "deprecated") {
					errors = append(errors, w)
				}
			}

			// ValidateAll path: when the config file has semantic errors we use an
			// empty config so the individual semantic errors (already surfaced in
			// the lump "config file: ..." line above) are not re-enumerated here.
			// Syntax-error case behaves identically: empty config, env still
			// validated against Defaults.
			resolveConfig := cfg
			if syntaxError || (err != nil && result != nil) {
				resolveConfig = &config.Config{}
			}
			resolved := config.Resolve(resolveConfig, envState, config.FlagState{}, config.Defaults)
			validationErrs := resolved.ValidateAll()
			errors = append(errors, validationErrs...)
			// Round-13 F#2: run ValidateRuntime against cfg+env whenever cfg is at
			// least parseable (result != nil), so env-only cross_check issues are
			// caught even when the YAML has unrelated semantic errors. Skip only
			// on true syntax error where we have no usable cfg — ValidateRuntime
			// against Defaults would false-positive (e.g. cross_check.enabled=true
			// default + cross_check.model="" default).
			if !syntaxError {
				runtimeResolved := resolved
				if err != nil && result != nil {
					// Semantic-error case: ValidateAll ran against empty cfg to
					// avoid duplication, but ValidateRuntime needs the real cfg
					// to honor user intent for cross_check config.
					runtimeResolved = config.Resolve(cfg, envState, config.FlagState{}, config.Defaults)
				}
				runtimeErrs := runtimeResolved.ValidateRuntime()
				errors = append(errors, runtimeErrs...)
			}

			// Validate guidance file is readable (uses same resolution logic as runtime)
			_, guidanceErr := config.ResolveGuidance(cfg, envState, config.FlagState{}, config.Defaults, configDir)
			if guidanceErr != nil {
				errors = append(errors, guidanceErr.Error())
			}

			// Report warnings
			for _, w := range warnings {
				logger.Logf(terminal.StyleWarning, "Config: %s", w)
			}

			// Report errors
			for _, e := range errors {
				logger.Logf(terminal.StyleError, "%s", e)
			}

			if len(errors) > 0 {
				return fmt.Errorf("configuration has %d error(s)", len(errors))
			}

			if len(warnings) > 0 {
				logger.Log("Configuration is valid (with warnings).", terminal.StyleSuccess)
			} else {
				logger.Log("Configuration is valid.", terminal.StyleSuccess)
			}

			return nil
		},
	}
}
