package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/feedback"
	"github.com/richhaase/agentic-code-reviewer/internal/filter"
	"github.com/richhaase/agentic-code-reviewer/internal/fpfilter"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/modelconfig"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

// cliOrLegacy returns (cliModel, legacyModel) based on whether the value came
// from a CLI flag or env var (highest priority) vs config file (lowest priority).
func cliOrLegacy(value string, fromCLI bool) (cli, legacy string) {
	if fromCLI {
		return value, ""
	}
	return "", value
}

func agentOptions(model, effort string, opts ReviewOpts) agent.AgentOptions {
	return agent.AgentOptions{
		Model:     model,
		Effort:    effort,
		CodexHome: opts.CodexHome,
	}
}

func executeReview(ctx context.Context, opts ReviewOpts, logger *terminal.Logger) domain.ExitCode {
	if err := agent.ValidateAgentNames(opts.ReviewerAgents); err != nil {
		logger.Logf(terminal.StyleError, "Invalid agent: %v", err)
		return domain.ExitError
	}

	// Early CLI availability preflight when auto-phase is active.
	// Auto-phase may invoke any configured agent role (arch, diff, cross-check)
	// depending on diff size, so verify all CLIs upfront to avoid wasting
	// review cycles on a missing binary.
	if shouldUseAutoPhase(opts) {
		if err := agent.CheckCLIAvailability(collectAllCLINames(opts)); err != nil {
			logger.Logf(terminal.StyleError, "Preflight check failed: %v", err)
			return domain.ExitError
		}
	}

	// Resolve the base ref once before launching parallel reviewers.
	// This ensures all reviewers compare against the same ref, avoiding
	// inconsistent results if network conditions vary during parallel execution.
	// Hoisted above agent construction so diff-size classification can feed the
	// size-aware model resolver (models.sizes.<size>.<role>).
	resolvedBaseRef := opts.Base
	if opts.Fetch {
		// Update current branch from remote (fast-forward only).
		// Skip when the base ref is relative to HEAD (e.g., HEAD~3) since
		// fast-forwarding would change what those refs resolve to.
		if !git.IsRelativeRef(opts.Base) {
			branchResult := git.UpdateCurrentBranch(ctx, opts.WorkDir)
			if branchResult.Updated && opts.Verbose {
				logger.Logf(terminal.StyleDim, "Updated branch %s from origin", branchResult.BranchName)
			}
			if branchResult.Error != nil {
				logger.Logf(terminal.StyleWarning, "Could not update %s from origin: %v (reviewing local state)", branchResult.BranchName, branchResult.Error)
			}
		}

		// Fetch base ref
		result := git.FetchRemoteRef(ctx, opts.Base, opts.WorkDir)
		resolvedBaseRef = result.ResolvedRef
		if result.FetchAttempted && !result.RefResolved {
			logger.Logf(terminal.StyleWarning, "Failed to fetch %s from origin, comparing against local %s (may be stale)", opts.Base, resolvedBaseRef)
		} else if opts.Verbose && result.FetchAttempted && result.RefResolved {
			logger.Logf(terminal.StyleDim, "Comparing against %s (fetched from origin)", resolvedBaseRef)
		}
	}

	// Classify diff size once up-front so the size-aware model resolver
	// (models.sizes.<size>.<role>) can pick per-role model/effort for every
	// agent constructed below. ClassifyDiffSize uses `git diff --stat` and is
	// cheap compared to GetDiff. On classification error we pass sizeStr=""
	// and the resolver falls through to defaults + legacy fields.
	var sizeStr string
	var diffSize git.DiffSize
	var diffFileCount, diffLineCount int
	var diffSizeClassified bool
	var classifyErr error
	diffSize, diffFileCount, diffLineCount, classifyErr = git.ClassifyDiffSize(ctx, resolvedBaseRef, opts.WorkDir)
	if classifyErr == nil {
		sizeStr = diffSize.String()
		diffSizeClassified = true
	} else if opts.Verbose {
		logger.Logf(terminal.StyleWarning, "Diff size classification failed: %v (model resolver will skip size layer)", classifyErr)
	}

	// --phase large: override sizeStr so model resolution uses sizes.large.
	if opts.Phase == domain.SizeLarge && sizeStr != domain.SizeLarge {
		sizeStr = domain.SizeLarge
	}

	// Resolve generic (phase="") reviewer specs with per-agent-name effort/model
	// via the size-aware phase-aware model config resolver. This covers the flat
	// review path (auto-phase OFF / size=small / explicit `--phase diff`); the
	// grouped and phased paths below re-resolve per-phase from the same config
	// using phase="arch"/"diff".
	reviewerSpecs := make([]agent.AgentSpec, 0, len(opts.ReviewerAgents))
	for _, name := range opts.ReviewerAgents {
		cliRevModel, legacyRevModel := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
		spec := modelconfig.ResolveReviewer(
			opts.Models, sizeStr, "" /* flat */, name,
			cliRevModel, "",
			legacyRevModel, "",
		)
		reviewerSpecs = append(reviewerSpecs, agent.AgentSpec{
			Name:    name,
			Options: agentOptions(spec.Model, spec.Effort, opts),
		})
	}
	reviewAgents, err := agent.CreateAgentsFromSpecs(reviewerSpecs)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return domain.ExitError
	}

	// rebindSpecAgent re-resolves the reviewer model/effort for a given
	// (phase, agent name) pair and constructs a fresh Agent instance for the
	// spec. Captures surrounding scope (opts, sizeStr) so downstream phase
	// wiring stays concise. Falls back to the original Agent on error.
	rebindSpecAgent := func(origAgent agent.Agent, phase string) agent.Agent {
		if origAgent == nil {
			return origAgent
		}
		name := origAgent.Name()
		cliRevModel, legacyRevModel := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
		spec := modelconfig.ResolveReviewer(
			opts.Models, sizeStr, phase, name,
			cliRevModel, "",
			legacyRevModel, "",
		)
		a, err := agent.NewAgentWithOptions(name, agentOptions(spec.Model, spec.Effort, opts))
		if err != nil {
			return origAgent
		}
		return a
	}

	cliSumModel, legacySumModel := cliOrLegacy(opts.SummarizerModel, opts.SummarizerModelFromCLI)
	summSpec := modelconfig.Resolve(
		opts.Models, sizeStr, modelconfig.RoleSummarizer, opts.SummarizerAgent,
		cliSumModel, "",
		legacySumModel, "",
	)
	summarizerAgent, err := agent.NewAgentWithOptions(opts.SummarizerAgent, agentOptions(summSpec.Model, summSpec.Effort, opts))
	if err != nil {
		logger.Logf(terminal.StyleError, "Invalid summarizer agent: %v", err)
		return domain.ExitError
	}
	if err := summarizerAgent.IsAvailable(); err != nil {
		logger.Logf(terminal.StyleError, "%s CLI not found (summarizer): %v", opts.SummarizerAgent, err)
		return domain.ExitError
	}

	// Cross-check agent resolution and CLI availability checks are BOTH deferred
	// to the grouped path below (see `if useGroupedSpecs && opts.CrossCheckEnabled`).
	// Cross-check fires only when useGroupedSpecs=true && sizeStr=="large", which
	// is reachable via auto-phase (DiffSizeLarge) or explicit --phase large.
	// Eager resolution here would force users on small/medium diffs (or
	// --phase small/medium / --no-auto-phase without --phase large / medium
	// fallback paths) to satisfy cross_check.model purely to start the run —
	// even though cross-check would never execute on those paths. Deferring
	// resolve until the gate tightens validate↔runtime symmetry: at the gate,
	// sizeStr is guaranteed to be "large", matching
	// `canResolveCrossCheckModelForAgent`'s sizes["large"] cascade in
	// internal/config. Cross-check model resolution and agent/model count
	// pairing are enforced at startup by shouldRunRuntimeValidation →
	// ResolvedConfig.ValidateRuntime for both auto-phase and --phase large.

	// Verbose: log the effective model/effort matrix for all roles once, up-front.
	// fp_filter and pr_feedback specs are resolved later in the flow, so we
	// re-invoke Resolve here (pure, cheap) solely for display purposes.
	if opts.Verbose {
		cliSumModelLog, legacySumModelLog := cliOrLegacy(opts.SummarizerModel, opts.SummarizerModelFromCLI)
		fpAgentLog := opts.FPFilterAgent
		if fpAgentLog == "" {
			fpAgentLog = opts.SummarizerAgent
		}
		fpModelLog := opts.FPFilterModel
		fpModelFromCLILog := opts.FPFilterModelFromCLI
		if fpModelLog == "" {
			fpModelLog = opts.SummarizerModel
			fpModelFromCLILog = opts.SummarizerModelFromCLI
		}
		cliFPModelLog, legacyFPModelLog := cliOrLegacy(fpModelLog, fpModelFromCLILog)
		cliFPEffortLog, legacyFPEffortLog := cliOrLegacy(opts.FPFilterEffort, opts.FPFilterEffortFromCLI)
		fpSpecLog := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RoleFPFilter, fpAgentLog,
			cliFPModelLog, cliFPEffortLog,
			legacyFPModelLog, legacyFPEffortLog,
		)
		prFeedbackAgentName := opts.PRFeedbackAgent
		if prFeedbackAgentName == "" {
			prFeedbackAgentName = opts.SummarizerAgent
		}
		prFeedbackSpecLog := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RolePRFeedback, prFeedbackAgentName,
			cliSumModelLog, "",
			legacySumModelLog, "",
		)
		logger.Logf(terminal.StyleDim, "Effective model matrix (size=%s):", formatSizeStr(sizeStr))
		for _, s := range reviewerSpecs {
			logger.Logf(terminal.StyleDim, "  reviewer[%s]       : %s", s.Name, formatSpec(agent.AgentOptions{Model: s.Options.Model, Effort: s.Options.Effort}))
		}
		// arch_reviewer / diff_reviewer rows intentionally omitted here: they
		// depend on whether the grouped path will actually execute, which is
		// decided later by resolveAutoPhase (needs the full diff to know
		// effectiveGroups). When grouped path is selected, the per-phase
		// rows are emitted alongside the "Auto-phase: grouped" line below.
		logger.Logf(terminal.StyleDim, "  summarizer        : %s", formatSpec(agent.AgentOptions{Model: summSpec.Model, Effort: summSpec.Effort}))
		// cross_check row intentionally omitted here: cross-check only runs when
		// useGroupedSpecs=true (resolveAutoPhase, large diff). Like
		// arch_reviewer / diff_reviewer rows, it is emitted alongside the
		// "Auto-phase: grouped" line below when grouped path is confirmed.
		logger.Logf(terminal.StyleDim, "  fp_filter         : %s", formatSpec(agent.AgentOptions{Model: fpSpecLog.Model, Effort: fpSpecLog.Effort}))
		logger.Logf(terminal.StyleDim, "  pr_feedback       : %s", formatSpec(agent.AgentOptions{Model: prFeedbackSpecLog.Model, Effort: prFeedbackSpecLog.Effort}))
	}

	// Pre-compute the git diff once and share it across all reviewers.
	// Always compute (even for codex-only) so we can short-circuit empty diffs.
	diff, err := git.GetDiff(ctx, resolvedBaseRef, opts.WorkDir)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to get diff: %v", err)
		return domain.ExitError
	}

	// Short-circuit: no changes means nothing to review
	if diff == "" {
		logger.Logf(terminal.StyleSuccess, "No changes detected between HEAD and %s. Nothing to review.", resolvedBaseRef)
		return domain.ExitNoFindings
	}

	if opts.Verbose {
		logger.Logf(terminal.StyleDim, "Diff size: %d bytes", len(diff))
	}

	// Pass precomputed diff to agents that need it (Claude, Gemini).
	// Codex ignores it (built-in diff via --base).
	diffPrecomputed := agent.AgentsNeedDiff(reviewAgents)

	if opts.Verbose && opts.UseRefFile {
		logger.Logf(terminal.StyleDim, "Ref-file mode enabled")
	}

	// Resolve review phases. phaseFromAuto tracks whether the final phase
	// configuration came from auto-phase (true) vs CLI `--phase` (false).
	// Only auto-phase-derived multi-phase runs consult arch_reviewer /
	// diff_reviewer; explicit `--phase` flags use the generic reviewer role.
	phaseStr := opts.Phase
	phaseFromAuto := false
	var useGroupedSpecs bool
	var groupedSpecs []runner.ReviewerSpec
	if shouldUseAutoPhase(opts) {
		if !diffSizeClassified {
			if opts.Verbose {
				logger.Logf(terminal.StyleWarning, "Auto-phase: diff size classification failed earlier, using default phase")
			}
		} else {
			size := diffSize
			fileCount := diffFileCount
			lineCount := diffLineCount
			if opts.Verbose {
				logger.Logf(terminal.StyleDim, "Auto-phase: diff is %s (%d files, %d lines)", size, fileCount, lineCount)
			}
			// Defer per-phase agent construction to resolveAutoPhase so that
			// IsAvailable() only runs once grouped path is actually viable.
			// Small/medium/large→fallback paths never call the closure, so
			// arch_reviewer_agent / diff_reviewer_agents CLI presence is not
			// required unless the grouped path will actually execute.
			apr, agentsErr := resolveAutoPhase(size, diff, opts.Guidance, diffPrecomputed,
				func() (agent.Agent, []agent.Agent, error) {
					return buildPhaseAgents(opts, sizeStr)
				},
				opts.LargeDiffReviewers, opts.MediumDiffReviewers,
				opts.MinLargeDiffReviewers, opts.MinMediumDiffReviewers)
			if agentsErr != nil {
				logger.Logf(terminal.StyleError, "Failed to build per-phase reviewer agents: %v", agentsErr)
				return domain.ExitError
			}
			phaseStr = apr.PhaseStr
			groupedSpecs = apr.GroupedSpecs
			useGroupedSpecs = apr.UseGrouped
			// Enable per-phase role rebinding (arch_reviewer/diff_reviewer) only
			// for medium/large diffs. Small diffs use flat diff review with the
			// generic reviewer role.
			phaseFromAuto = size != git.DiffSizeSmall
			if opts.Verbose {
				switch {
				case apr.UseGrouped && sizeStr == domain.SizeLarge:
					logger.Logf(terminal.StyleInfo, "Auto-phase: grouped (arch+%d diff groups)",
						len(groupedSpecs)-1)
					logPerPhaseModelMatrix(logger, opts, sizeStr)
					if opts.CrossCheckEnabled {
						logCrossCheckModelMatrix(logger, opts, sizeStr)
					}
				case apr.UseGrouped:
					logger.Logf(terminal.StyleInfo, "Auto-phase: medium split (arch+%d diff groups)",
						len(groupedSpecs)-1)
					logPerPhaseModelMatrix(logger, opts, sizeStr)
				case apr.MediumDiffCount > 0 && apr.FallbackReason != "":
					logger.Logf(terminal.StyleWarning,
						"Auto-phase: medium (medium_diff_reviewers=%d) — fallback: %s",
						apr.MediumDiffCount, apr.FallbackReason)
				case apr.MediumDiffCount > 0:
					logger.Logf(terminal.StyleInfo,
						"Auto-phase: medium (medium_diff_reviewers=%d)", apr.MediumDiffCount)
				case apr.FallbackReason != "":
					logger.Logf(terminal.StyleWarning, "Auto-phase: falling back to medium (%s)", apr.FallbackReason)
				}
			}
		}
	}

	// --phase large: build grouped specs before dispatch (defensive fallback to medium)
	if phaseStr == domain.SizeLarge && !useGroupedSpecs {
		plr, agentsErr := resolvePhaseLarge(diff, opts.Guidance, diffPrecomputed,
			func() (agent.Agent, []agent.Agent, error) {
				return buildPhaseAgents(opts, sizeStr)
			},
			opts.LargeDiffReviewers, opts.MinLargeDiffReviewers, opts.MediumDiffReviewers)
		if agentsErr != nil {
			logger.Logf(terminal.StyleError, "Failed to build per-phase agents for --phase large: %v", agentsErr)
			return domain.ExitError
		}
		if plr.UseGrouped {
			groupedSpecs = plr.GroupedSpecs
			useGroupedSpecs = true
			if opts.Verbose {
				effectiveReviewers := opts.LargeDiffReviewers
				if effectiveReviewers < opts.MinLargeDiffReviewers {
					effectiveReviewers = opts.MinLargeDiffReviewers
					logger.Logf(terminal.StyleDim,
						"--phase large: large_diff_reviewers=%d clamped to min_large_diff_reviewers=%d",
						opts.LargeDiffReviewers, effectiveReviewers)
				}
				logger.Logf(terminal.StyleInfo,
					"--phase large: grouped (effective_reviewers=%d, arch+%d diff groups)",
					effectiveReviewers, len(groupedSpecs)-1)
				logPerPhaseModelMatrix(logger, opts, sizeStr)
				if opts.CrossCheckEnabled {
					logCrossCheckModelMatrix(logger, opts, sizeStr)
				}
			}
		} else {
			phaseStr = plr.PhaseStr
			logger.Logf(terminal.StyleWarning,
				"--phase large: falling back to %s (%s)", plr.PhaseStr, plr.FallbackReason)
			if opts.CrossCheckEnabled {
				logger.Logf(terminal.StyleWarning,
					"--phase large: cross-check requires grouped review; skipped after fallback to %s", plr.PhaseStr)
			}
		}
	}

	// Build runner: phase-based (NewWithSpecs) or legacy (New)
	runnerConfig := runner.Config{
		Reviewers:       opts.Reviewers,
		Concurrency:     opts.Concurrency,
		BaseRef:         resolvedBaseRef,
		Timeout:         opts.Timeout,
		Retries:         opts.Retries,
		Verbose:         opts.Verbose,
		WorkDir:         opts.WorkDir,
		Guidance:        opts.Guidance,
		UseRefFile:      opts.UseRefFile,
		Diff:            diff,
		DiffPrecomputed: diffPrecomputed,
		RolePrompts:     opts.RolePrompts,
	}

	var r *runner.Runner
	var distributionStr string
	actualReviewers := opts.Reviewers
	if useGroupedSpecs {
		// Auto-phase grouped path: per-phase model/effort rebind using
		// ResolveReviewer with spec.Phase ("arch"/"diff"). arch_reviewer /
		// diff_reviewer overrides take effect here.
		for i := range groupedSpecs {
			groupedSpecs[i].Agent = rebindSpecAgent(groupedSpecs[i].Agent, groupedSpecs[i].Phase)
		}
		actualReviewers = len(groupedSpecs)
		distributionStr = runner.FormatDistributionFromSpecs(groupedSpecs)
		runnerConfig.HasArchReviewer = specsHaveArch(groupedSpecs)
		r, err = runner.NewWithSpecs(runnerConfig, groupedSpecs, logger)
	} else if phaseStr != "" {
		totalForParse := opts.Reviewers
		switch phaseStr {
		case domain.SizeSmall:
			totalForParse = opts.SmallDiffReviewers
		case domain.SizeMedium:
			totalForParse = opts.MediumDiffReviewers + 1
		}
		phases, phaseErr := parsePhases(phaseStr, totalForParse)
		if phaseErr != nil {
			logger.Logf(terminal.StyleError, "Invalid --phase: %v", phaseErr)
			return domain.ExitError
		}
		if opts.Verbose {
			for _, p := range phases {
				logger.Logf(terminal.StyleDim, "Phase %q: %d reviewer(s)", p.Phase, p.ReviewerCount)
			}
		}
		specs, specErr := runner.BuildReviewerSpecs(phases, reviewAgents, opts.Guidance, diff, diffPrecomputed)
		if specErr != nil {
			logger.Logf(terminal.StyleError, "Phase config error: %v", specErr)
			return domain.ExitError
		}
		if phaseFromAuto {
			for i := range specs {
				specs[i].Agent = rebindSpecAgent(specs[i].Agent, specs[i].Phase)
			}
		}
		actualReviewers = len(specs)
		distributionStr = runner.FormatDistributionFromSpecs(specs)
		runnerConfig.HasArchReviewer = specsHaveArch(specs)
		r, err = runner.NewWithSpecs(runnerConfig, specs, logger)
	} else {
		if len(reviewAgents) > 1 {
			distributionStr = agent.FormatDistribution(reviewAgents, opts.Reviewers)
		}
		// Flat path: no arch reviewer, HasArchReviewer stays false (default).
		r, err = runner.New(runnerConfig, reviewAgents, logger)
	}

	if err != nil {
		logger.Logf(terminal.StyleError, "Runner initialization failed: %v", err)
		return domain.ExitError
	}

	if distributionStr != "" {
		logger.Logf(terminal.StyleInfo, "Agent distribution: %s%s%s",
			terminal.Color(terminal.Dim), distributionStr, terminal.Color(terminal.Reset))
	} else if opts.Verbose && len(opts.ReviewerAgents) > 0 {
		logger.Logf(terminal.StyleDim, "%sUsing agent: %s%s",
			terminal.Color(terminal.Dim), opts.ReviewerAgents[0], terminal.Color(terminal.Reset))
	}

	if opts.Concurrency < actualReviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), actualReviewers, opts.Concurrency, opts.Base, terminal.Color(terminal.Reset))
	} else {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
			terminal.Color(terminal.Dim), actualReviewers, opts.Base, terminal.Color(terminal.Reset))
	}

	// Start PR feedback summarizer in parallel with reviewers (if enabled, reviewing a PR, and FP filter is on)
	// Skip if FP filter is disabled since the feedback summary is only consumed by the FP filter
	var priorFeedback string
	var feedbackWg sync.WaitGroup
	if opts.PRFeedbackEnabled && opts.DetectedPR != "" && opts.FPFilterEnabled {
		logger.Logf(terminal.StyleInfo, "Summarizing PR #%s feedback %s(in parallel)%s",
			opts.DetectedPR, terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		feedbackWg.Add(1)
		go func() {
			defer feedbackWg.Done()

			// Determine which agent to use for feedback summarization
			feedbackAgentName := opts.PRFeedbackAgent
			if feedbackAgentName == "" {
				feedbackAgentName = opts.SummarizerAgent
			}

			cliSumModelPR, legacySumModelPR := cliOrLegacy(opts.SummarizerModel, opts.SummarizerModelFromCLI)
			prSpec := modelconfig.Resolve(
				opts.Models, sizeStr, modelconfig.RolePRFeedback, feedbackAgentName,
				cliSumModelPR, "",
				legacySumModelPR, "",
			)
			summarizer := feedback.NewSummarizer(feedbackAgentName, prSpec.Model, prSpec.Effort, opts.CodexHome, opts.Verbose, logger)
			feedbackCtx, feedbackCancel := context.WithTimeout(ctx, opts.SummarizerTimeout)
			defer feedbackCancel()

			summary, err := summarizer.Summarize(feedbackCtx, opts.DetectedPR)
			if err != nil {
				// Distinguish feedback-specific timeout from parent context cancellation
				if ctx.Err() != nil {
					// Parent context was canceled (e.g., user interrupt) — don't log as timeout
					return
				}
				if feedbackCtx.Err() == context.DeadlineExceeded {
					logger.Logf(terminal.StyleWarning, "PR feedback summarizer timed out after %s", opts.SummarizerTimeout)
					return
				}
				logger.Logf(terminal.StyleWarning, "PR feedback summarizer failed: %v", err)
				return
			}
			if summary != "" {
				logger.Log("PR feedback summarized", terminal.StyleSuccess)
			} else {
				logger.Log("No relevant PR feedback found", terminal.StyleDim)
			}
			priorFeedback = summary
		}()
	}

	results, wallClock, err := r.Run(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}

	// Build statistics
	stats := runner.BuildStats(results, actualReviewers, wallClock)

	// Check if all reviewers failed
	if stats.AllFailed() {
		logger.Log("All reviewers failed", terminal.StyleError)
		return domain.ExitError
	}

	// Aggregate and summarize findings
	allFindings := runner.CollectFindings(results)
	aggregated := domain.AggregateFindings(allFindings)

	// Cross-check: run only for large grouped diff reviews with >=2 groups.
	// Medium grouped specs also set useGroupedSpecs=true but cross-check
	// config/model resolution only supports sizes.large.
	var ccResult *summarizer.CrossCheckResult
	if useGroupedSpecs && sizeStr == domain.SizeLarge && opts.CrossCheckEnabled && ctx.Err() == nil {
		// Lazy resolve: top-level cross_check.model parsing and per-agent
		// modelconfig.Resolve happen here, NOT at startup. The sizeStr=="large"
		// guard ensures the size layer cascade lines up with
		// canResolveCrossCheckModelForAgent's sizes["large"] view.
		ccAgentNames, ccModels, ccErr := resolveCrossCheckAgents(opts, sizeStr)
		if ccErr != nil {
			logger.Logf(terminal.StyleError, "%v", ccErr)
			return domain.ExitError
		}
		// Per-agent model is positionally paired with the agent name.
		ccSpecs := make([]summarizer.CrossCheckAgentSpec, 0, len(ccAgentNames))
		for i, name := range ccAgentNames {
			cliCCModel, legacyCCModel := cliOrLegacy(ccModels[i], opts.CrossCheckModelFromCLI)
			perSpec := modelconfig.Resolve(
				opts.Models, sizeStr, modelconfig.RoleCrossCheck, name,
				cliCCModel, "",
				legacyCCModel, "",
			)
			ccSpecs = append(ccSpecs, summarizer.CrossCheckAgentSpec{
				Name:    name,
				Options: summarizer.CrossCheckOptions{Model: perSpec.Model, Effort: perSpec.Effort, CodexHome: opts.CodexHome},
			})
		}
		// Lazy CLI availability check: only verify cross-check agent CLIs are
		// installed when we are about to actually run cross-check. Non-grouped
		// paths (small diff / explicit --phase / --no-auto-phase / medium)
		// skip this entirely so users without the cross-check CLI can still
		// run those review modes.
		if err := agent.ValidateAgentNames(ccAgentNames); err != nil {
			logger.Logf(terminal.StyleError, "Invalid cross-check agent: %v", err)
			return domain.ExitError
		}
		for _, spec := range ccSpecs {
			ccAg, err := agent.NewAgentWithOptions(spec.Name, agent.AgentOptions{Model: spec.Options.Model, Effort: spec.Options.Effort, CodexHome: spec.Options.CodexHome})
			if err != nil {
				logger.Logf(terminal.StyleError, "Invalid cross-check agent %q: %v", spec.Name, err)
				return domain.ExitError
			}
			if err := ccAg.IsAvailable(); err != nil {
				logger.Logf(terminal.StyleError, "%s CLI not found (cross-check): %v", spec.Name, err)
				return domain.ExitError
			}
		}

		ccCtx := buildCrossCheckContext(aggregated, groupedSpecs, results)
		if len(ccCtx.Groups) >= 2 {
			ccSpinner := terminal.NewPhaseSpinner("Cross-checking groups")
			ccSpinnerCtx, ccSpinnerCancel := context.WithCancel(ctx)
			ccSpinnerDone := make(chan struct{})
			go func() {
				ccSpinner.Run(ccSpinnerCtx)
				close(ccSpinnerDone)
			}()

			ccRunCtx, ccRunCancel := context.WithTimeout(ctx, opts.CrossCheckTimeout)
			ccResult = summarizer.CrossCheck(ccRunCtx, ccSpecs, ccCtx, opts.Verbose, logger)
			ccRunCancel()
			ccSpinnerCancel()
			<-ccSpinnerDone
			stats.CrossCheckDuration = ccResult.Duration

			if ccResult.Skipped && ctx.Err() == nil {
				if ccResult.SkipReason != "" {
					logger.Logf(terminal.StyleWarning, "Cross-check skipped (%s)", ccResult.SkipReason)
				}
			} else if ccResult.SkipReason != "" && ctx.Err() == nil {
				// Partial failure: some agents succeeded
				logger.Logf(terminal.StyleWarning, "Cross-check partial (%s)", ccResult.SkipReason)
			}
		}
	}

	// Run summarizer with spinner
	phaseSpinner := terminal.NewPhaseSpinner("Summarizing")
	spinnerCtx, spinnerCancel := context.WithCancel(context.Background())
	spinnerDone := make(chan struct{})
	go func() {
		phaseSpinner.Run(spinnerCtx)
		close(spinnerDone)
	}()

	summarizerCtx, summarizerCancel := context.WithTimeout(ctx, opts.SummarizerTimeout)
	defer summarizerCancel()

	summaryResult, err := summarizer.Summarize(summarizerCtx, opts.SummarizerAgent, summarizer.SummarizeOptions{Model: summSpec.Model, Effort: summSpec.Effort, CodexHome: opts.CodexHome}, aggregated, ccResult, opts.Verbose, logger)
	spinnerCancel()
	<-spinnerDone

	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		if summarizerCtx.Err() == context.DeadlineExceeded {
			logger.Logf(terminal.StyleError, "Summarizer timed out after %s", opts.SummarizerTimeout)
		} else {
			logger.Logf(terminal.StyleError, "Summarizer error: %v", err)
		}
		return domain.ExitError
	}

	stats.SummarizerDuration = summaryResult.Duration

	// Backfill per-phase reviewer counts for role-separated display
	reviewerPhases := make(map[int]string, len(results))
	for _, r := range results {
		if r.Phase != "" {
			reviewerPhases[r.ReviewerID] = r.Phase
		}
	}
	if len(reviewerPhases) > 0 {
		domain.BackfillPhaseReviewerCounts(&summaryResult.Grouped, aggregated, reviewerPhases)
	}

	// Wait for PR feedback summarizer to complete
	feedbackWg.Wait()

	var fpFilteredCount int
	var fpRemoved []domain.FPRemovedInfo
	var noiseRemoved []domain.FPRemovedInfo
	var noiseFindingsForDisplay []domain.FindingGroup
	if opts.FPFilterEnabled && summaryResult.ExitCode == 0 && len(summaryResult.Grouped.Findings) > 0 && ctx.Err() == nil {
		fpSpinner := terminal.NewPhaseSpinner("Filtering false positives")
		fpSpinnerCtx, fpSpinnerCancel := context.WithCancel(ctx)
		fpSpinnerDone := make(chan struct{})
		go func() {
			fpSpinner.Run(fpSpinnerCtx)
			close(fpSpinnerDone)
		}()

		fpCtx, fpCancel := context.WithTimeout(ctx, opts.FPFilterTimeout)
		defer fpCancel()
		fpAgentName := opts.FPFilterAgent
		if fpAgentName == "" {
			fpAgentName = opts.SummarizerAgent
		}
		if fpAgentName != opts.SummarizerAgent {
			fpAg, err := agent.NewAgentWithOptions(fpAgentName, agent.AgentOptions{CodexHome: opts.CodexHome})
			if err != nil {
				fpSpinnerCancel()
				<-fpSpinnerDone
				logger.Logf(terminal.StyleError, "Invalid FP filter agent: %v", err)
				return domain.ExitError
			}
			if err := fpAg.IsAvailable(); err != nil {
				fpSpinnerCancel()
				<-fpSpinnerDone
				logger.Logf(terminal.StyleError, "%s CLI not found (fp-filter): %v", fpAgentName, err)
				return domain.ExitError
			}
		}

		fpModel := opts.FPFilterModel
		fpModelFromCLI := opts.FPFilterModelFromCLI
		if fpModel == "" {
			fpModel = opts.SummarizerModel
			fpModelFromCLI = opts.SummarizerModelFromCLI
		}
		cliFPModel, legacyFPModel := cliOrLegacy(fpModel, fpModelFromCLI)
		cliFPEffort, legacyFPEffort := cliOrLegacy(opts.FPFilterEffort, opts.FPFilterEffortFromCLI)

		fpSpec := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RoleFPFilter, fpAgentName,
			cliFPModel, cliFPEffort,
			legacyFPModel, legacyFPEffort,
		)
		fpFilter := fpfilter.New(fpAgentName, fpSpec.Model, fpSpec.Effort, opts.CodexHome, opts.FPThreshold, opts.TriageEnabled, opts.Verbose, logger)
		fpResult := fpFilter.Apply(fpCtx, summaryResult.Grouped, priorFeedback, stats.SuccessfulReviewers)
		fpSpinnerCancel()
		<-fpSpinnerDone

		if fpResult != nil && fpResult.Skipped && ctx.Err() == nil {
			logger.Logf(terminal.StyleWarning, "FP filter skipped (%s): showing all findings", fpResult.SkipReason)
		}
		if fpResult != nil {
			summaryResult.Grouped = fpResult.Grouped
			fpFilteredCount = fpResult.RemovedCount
			stats.FPFilterDuration = fpResult.Duration

			for _, r := range fpResult.Removed {
				fpRemoved = append(fpRemoved, domain.FPRemovedInfo{
					Sources:   r.Finding.Sources,
					FPScore:   r.FPScore,
					Reasoning: r.Reasoning,
					Title:     r.Finding.Title,
				})
			}

			for _, n := range fpResult.Noise {
				noiseRemoved = append(noiseRemoved, domain.FPRemovedInfo{
					Sources:   n.Finding.Sources,
					FPScore:   n.FPScore,
					Reasoning: n.Reasoning,
					Title:     n.Finding.Title,
				})
				noiseFindingsForDisplay = append(noiseFindingsForDisplay, n.Finding)
			}
			stats.NoiseFilteredCount = fpResult.NoiseCount

			if opts.ShowNoise && len(fpResult.Noise) > 0 {
				for _, n := range fpResult.Noise {
					summaryResult.Grouped.Findings = append(summaryResult.Grouped.Findings, n.Finding)
				}
			}
		}
	}
	stats.FPFilteredCount = fpFilteredCount

	if ctx.Err() != nil {
		return domain.ExitInterrupted
	}

	var excludeFiltered []domain.FindingGroup
	if len(opts.ExcludePatterns) > 0 {
		f, err := filter.New(opts.ExcludePatterns)
		if err != nil {
			logger.Logf(terminal.StyleError, "Invalid exclude pattern: %v", err)
			return domain.ExitError
		}
		preExclude := summaryResult.Grouped.Findings
		summaryResult.Grouped = f.Apply(summaryResult.Grouped)
		excludeFiltered = diffFindingGroups(preExclude, summaryResult.Grouped.Findings)
	}

	// Build disposition map for LGTM annotation
	dispositions := domain.BuildDispositions(
		len(aggregated),
		summaryResult.Grouped.Info,
		fpRemoved,
		noiseRemoved,
		excludeFiltered,
		summaryResult.Grouped.Findings,
	)
	// Severity reconcile now lives in summarizer.backfillSeverity (3 rules:
	// empty Sources → blocking; any aggregated source blocking → blocking;
	// otherwise default to advisory). Keeping a second pass here would
	// double-apply and conflict with the single source of truth.

	// Compute overall Verdict (and Ok) from post-filter state.
	// Cross-check degradation (Partial or non-structural Skipped) is folded
	// into ccAdvisory so a clean grouped run cannot LGTM while the cross-check
	// layer silently lost coverage (split-brain prevention). Keeping domain
	// ComputeVerdict pure, degradation interpretation lives at this boundary.
	ccBlocking := ccResult.HasBlockingFindings()
	ccAdvisory := ccResult != nil && !ccBlocking && (ccResult.HasAdvisoryFindings() || ccResult.IsDegraded())
	summaryResult.Grouped.ComputeVerdict(ccBlocking, ccAdvisory)

	// Inject noise findings for display AFTER verdict computation so they
	// don't affect exit codes or PR submission decisions.
	if opts.ShowNoise {
		summaryResult.Grouped.Findings = append(summaryResult.Grouped.Findings, noiseFindingsForDisplay...)
	}

	// Render and print report
	if opts.Format == "json" {
		jsonBytes, jsonErr := runner.RenderJSON(&summaryResult.Grouped, ccResult)
		if jsonErr != nil {
			logger.Logf(terminal.StyleError, "JSON rendering failed: %v", jsonErr)
			return domain.ExitError
		}
		fmt.Println(string(jsonBytes))
	} else {
		report := runner.RenderReport(summaryResult.Grouped, summaryResult, stats, ccResult)
		fmt.Println(report)
	}

	if summaryResult.ExitCode != 0 {
		return domain.ExitError
	}

	// Handle PR actions based on overall verdict.
	// "ok"        → LGTM path, exit 0
	// "advisory"  → findings path, exit 0 (unless --strict, then 1)
	// "blocking"  → findings path, exit 1
	verdict := summaryResult.Grouped.Verdict
	if verdict == "ok" {
		return handleLGTM(ctx, opts, allFindings, aggregated, dispositions, stats, logger)
	}

	findingsCode, finalVerdict := handleFindings(ctx, opts, summaryResult.Grouped, aggregated, ccResult, ccBlocking, ccAdvisory, verdict, opts.Strict, stats, logger)
	return applyVerdictExitPolicy(finalVerdict, opts.Strict, findingsCode)
}

// applyVerdictExitPolicy maps a verdict + strict flag + handleFindings exit code
// to the final exit code per Part C policy:
//
//	verdict=="ok"       → findingsCode unchanged (caller handles LGTM branch)
//	verdict=="advisory" → 0 unless strict; strict keeps findingsCode
//	verdict=="blocking" → findingsCode unchanged (1 on findings)
//	propagates non-ExitFindings codes (e.g. interrupted/error) unchanged
func applyVerdictExitPolicy(verdict string, strict bool, findingsCode domain.ExitCode) domain.ExitCode {
	if verdict == "advisory" && !strict && findingsCode == domain.ExitFindings {
		return domain.ExitNoFindings
	}
	return findingsCode
}

// shouldUseAutoPhase returns true when auto-phase is enabled and no explicit
// --phase override was provided. Explicit --phase always takes precedence.
func shouldUseAutoPhase(opts ReviewOpts) bool {
	return opts.AutoPhase && opts.Phase == ""
}

// specsHaveArch returns true if any spec in the slice has Phase domain.PhaseArch.
func specsHaveArch(specs []runner.ReviewerSpec) bool {
	for _, s := range specs {
		if s.Phase == domain.PhaseArch {
			return true
		}
	}
	return false
}

// shouldRunRuntimeValidation returns true when ValidateRuntime() should be
// called at startup. Cross-check can fire on two paths: (1) auto-phase when
// the diff is classified as large, and (2) explicit --phase large. All other
// paths (--phase small/medium, --no-auto-phase without --phase large) cannot
// trigger cross-check, so validation is safely skipped.
func shouldRunRuntimeValidation(autoPhase bool, phaseFlag string) bool {
	return (autoPhase && phaseFlag == "") || phaseFlag == domain.SizeLarge
}

// collectAllCLINames returns CLI names that may be invoked during an auto-phase
// review run. The returned slice may contain duplicates; deduplication is handled
// by CheckCLIAvailability. Fallback defaults for optional fields are resolved here
// (arch_reviewer_agent → ReviewerAgents[0], cross_check.agent → SummarizerAgent, etc.).
func collectAllCLINames(opts ReviewOpts) []string {
	names := make([]string, 0, 8)
	names = append(names, opts.ReviewerAgents...)
	names = append(names, opts.SummarizerAgent)

	// Arch reviewer: explicit override or first reviewer agent
	archName := opts.ArchReviewerAgent
	if archName == "" && len(opts.ReviewerAgents) > 0 {
		archName = opts.ReviewerAgents[0]
	}
	if archName != "" {
		names = append(names, archName)
	}

	// Diff reviewers: explicit override or all reviewer agents
	diffNames := opts.DiffReviewerAgents
	if len(diffNames) == 0 {
		diffNames = opts.ReviewerAgents
	}
	names = append(names, diffNames...)

	// Cross-check: only if enabled; parse comma-separated names
	if opts.CrossCheckEnabled {
		ccName := opts.CrossCheckAgent
		if ccName == "" {
			ccName = opts.SummarizerAgent
		}
		names = append(names, agent.ParseAgentNames(ccName)...)
	}

	// FP filter: only if enabled and using a different agent
	if opts.FPFilterEnabled && opts.FPFilterAgent != "" {
		names = append(names, opts.FPFilterAgent)
	}

	return names
}

// isLGTM reports whether the review result qualifies for LGTM:
// no grouped findings AND no blocking cross-check findings.
// ccResult may be nil (nil-safe via CrossCheckResult.HasBlockingFindings).
func isLGTM(grouped domain.GroupedFindings, cc *summarizer.CrossCheckResult) bool {
	return !grouped.HasFindings() && !cc.HasBlockingFindings()
}

// parsePhases converts a phase string into PhaseConfigs.
// "small" → N diff reviewers; "medium" → 1 arch + remaining diff.
// Returns an error for unknown phase values or insufficient reviewer budget.
func parsePhases(phaseStr string, totalReviewers int) ([]runner.PhaseConfig, error) {
	if totalReviewers < 1 {
		return nil, fmt.Errorf("totalReviewers must be >= 1, got %d", totalReviewers)
	}
	switch phaseStr {
	case domain.SizeSmall:
		return []runner.PhaseConfig{
			{Phase: domain.PhaseDiff, ReviewerCount: totalReviewers},
		}, nil
	case domain.SizeMedium:
		if totalReviewers < 2 {
			return nil, fmt.Errorf("phase %q requires >= 2 reviewers (1 arch + >= 1 diff), got %d", phaseStr, totalReviewers)
		}
		return []runner.PhaseConfig{
			{Phase: domain.PhaseArch, ReviewerCount: 1},
			{Phase: domain.PhaseDiff, ReviewerCount: totalReviewers - 1},
		}, nil
	default:
		return nil, fmt.Errorf("unknown phase %q (valid: small, medium)", phaseStr)
	}
}

// resolveCrossCheckAgents returns the resolved cross-check agent names and
// per-agent models. When opts.CrossCheckModel is non-empty it MUST be a
// comma-separated list whose count matches CrossCheckAgent (after fallback
// to SummarizerAgent when CrossCheckAgent is unset). Agents and models are
// paired positionally: agents[i] uses models[i].
//
// When opts.CrossCheckModel is empty, per-agent resolution falls back to the
// models cascade via modelconfig.Resolve(..., RoleCrossCheck, name, ...).
// This mirrors the validation in config.ValidateRuntime so "validates OK"
// implies "runs OK" whenever a user configures cross-check through the
// structured `models` tree (models.agents.<name>.cross_check / models.sizes.
// large.cross_check / models.defaults.cross_check) instead of the legacy
// top-level cross_check.model field. Before Round-13 the runtime required
// opts.CrossCheckModel unconditionally, producing a "validates OK but never
// runs" contract asymmetry (Round-13 F#1).
//
// Round-14 F#1 contract: this function is invoked exclusively from the
// grouped-path gate in executeReview, where sizeStr is guaranteed to be
// "large" (useGroupedSpecs=true is reachable via auto-phase DiffSizeLarge
// or explicit --phase large). The defense-in-depth log helper
// logCrossCheckModelMatrix also calls this with the same sizeStr just
// before the gate, so the size argument is consistent across both call
// sites. Non-grouped review paths (small / medium / --phase small|medium /
// --no-auto-phase without --phase large / medium fallback) skip cross-check
// entirely and never invoke this function.
//
// Returns (nil, nil, nil) when cross-check is disabled.
//
// NOTE: agent.ParseAgentNames("") returns []string{DefaultAgent} ("codex"),
// so we must check the raw string BEFORE calling ParseAgentNames to detect
// a truly-unset CrossCheckAgent and fall back to SummarizerAgent. This is
// belt-and-suspenders even if config.Resolve already copies the value,
// because the fallback here is the canonical resolution point.
func resolveCrossCheckAgents(opts ReviewOpts, sizeStr string) ([]string, []string, error) {
	if !opts.CrossCheckEnabled {
		return nil, nil, nil
	}
	agentSpec := opts.CrossCheckAgent
	if strings.TrimSpace(agentSpec) == "" {
		agentSpec = opts.SummarizerAgent
	}
	names := agent.ParseAgentNames(agentSpec)

	if strings.TrimSpace(opts.CrossCheckModel) != "" {
		// Explicit top-level model list: split, validate, pair positionally.
		rawModels := strings.Split(opts.CrossCheckModel, ",")
		models := make([]string, 0, len(rawModels))
		for _, m := range rawModels {
			m = strings.TrimSpace(m)
			if m == "" {
				return nil, nil, fmt.Errorf("--cross-check-model contains an empty entry; check for leading/trailing/duplicate commas")
			}
			models = append(models, m)
		}
		if len(names) != len(models) {
			return nil, nil, fmt.Errorf("--cross-check-agent (%d agents) and --cross-check-model (%d models) must have same count; agents and models are paired by position", len(names), len(models))
		}
		return names, models, nil
	}

	// Top-level unset: per-agent resolution via the models cascade. Each
	// selected agent must produce a non-empty Model at the runtime size.
	models := make([]string, len(names))
	var missing []string
	for i, name := range names {
		spec := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RoleCrossCheck, name,
			"", "", // no CLI override (top-level was empty)
			"", "", // no legacy fallback — the cross_check legacy path IS the top-level field we already checked
		)
		if strings.TrimSpace(spec.Model) == "" {
			missing = append(missing, name)
			continue
		}
		models[i] = spec.Model
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf(
			"cross_check.enabled=true requires cross_check.model for agent(s) %v "+
				"(supply via --cross-check-model / ACR_CROSS_CHECK_MODEL as a "+
				"comma-separated list paired 1:1 with --cross-check-agent, or via "+
				"models.{agents.<name>,sizes.large,defaults}.cross_check.model "+
				"— note: cross-check runs only at size=large, so sizes.small/medium "+
				"entries are dead config); "+
				"or disable cross-check with --no-cross-check",
			missing)
	}
	return names, models, nil
}

// buildCrossCheckContext assembles the cross-check input context from review
// specs, reviewer results, and aggregated findings.
func buildCrossCheckContext(findings []domain.AggregatedFinding, specs []runner.ReviewerSpec, results []domain.ReviewerResult) summarizer.CrossCheckContext {
	// Collect unique group keys from specs (preserving order of first appearance).
	groupInfos := make([]summarizer.GroupInfo, 0, len(specs))
	seenKey := make(map[string]bool, len(specs))
	for _, s := range specs {
		if s.GroupKey == "" || seenKey[s.GroupKey] {
			continue
		}
		seenKey[s.GroupKey] = true
		groupInfos = append(groupInfos, summarizer.GroupInfo{
			GroupKey:    s.GroupKey,
			Phase:       s.Phase,
			TargetFiles: s.TargetFiles,
			FullDiff:    s.Phase == domain.PhaseArch,
		})
	}

	// Aggregate outcomes by group key. A group succeeds if >=1 reviewer for it
	// returned exit 0, or produced findings despite non-zero exit, without
	// timeout/auth failure.
	outcomeByKey := make(map[string]*summarizer.GroupOutcome, len(groupInfos))
	for _, g := range groupInfos {
		outcomeByKey[g.GroupKey] = &summarizer.GroupOutcome{GroupKey: g.GroupKey}
	}
	// Map reviewer id -> group key via specs (uses authoritative ReviewerID field).
	idToKey := make(map[int]string, len(specs))
	for _, s := range specs {
		idToKey[s.ReviewerID] = s.GroupKey
	}
	for _, r := range results {
		key := idToKey[r.ReviewerID]
		o, ok := outcomeByKey[key]
		if !ok {
			continue
		}
		if r.TimedOut {
			o.TimedOut = true
		}
		if r.AuthFailed {
			o.AuthFailed = true
		}
		if !r.TimedOut && !r.AuthFailed {
			if r.ExitCode == 0 {
				o.Succeeded = true
			} else if len(r.Findings) > 0 {
				// Non-zero exit with findings: the reviewer ran far enough to
				// produce results (e.g., sandbox shell failure after review).
				// Treating this as a gap would re-introduce Issue #42.
				o.Succeeded = true
				o.PartialExit = true
			}
		}
		o.FindingCount += len(r.Findings)
	}

	outcomes := make([]summarizer.GroupOutcome, 0, len(groupInfos))
	for _, g := range groupInfos {
		outcomes = append(outcomes, *outcomeByKey[g.GroupKey])
	}

	return summarizer.CrossCheckContext{
		Findings: findings,
		Groups:   groupInfos,
		Outcomes: outcomes,
	}
}

// autoPhaseResult holds the outcome of resolveAutoPhase.
type autoPhaseResult struct {
	PhaseStr        string                // non-empty → use legacy phase path
	GroupedSpecs    []runner.ReviewerSpec // non-nil → use grouped diff path
	UseGrouped      bool
	FallbackReason  string // non-empty when UseGrouped was downgraded to false
	MediumDiffCount int    // 0 = not a medium fallback; otherwise the diff-phase reviewer count to use with parsePhases (arch=1 + this)
}

// resolveAutoPhase determines phase configuration based on diff size and the
// new auto-phase knobs.
//
//   - largeDiffReviewers: target group count for the grouped (large) path; capped by
//     the actual file count to keep at least 1 file per group.
//   - mediumDiffReviewers: diff-phase reviewer count for the medium path
//     (medium) and for any large fallback to medium.
//
// archAgent and diffAgents are passed through to buildGroupedDiffSpecs so the
// arch and diff phases can use distinct agent backends per
// arch_reviewer_agent / diff_reviewer_agents config.
//
// `reviewers` is intentionally NOT a parameter: --reviewers is now reserved
// for the flat path (auto-phase OFF / small / explicit --phase). Auto-phase
// medium/large paths derive their concurrency from these dedicated knobs.
func resolveAutoPhase(
	size git.DiffSize,
	diff, guidance string,
	diffPrecomputed bool,
	buildAgents func() (agent.Agent, []agent.Agent, error),
	largeDiffReviewers int,
	mediumDiffReviewers int,
	minLargeDiffReviewers int,
	minMediumDiffReviewers int,
) (autoPhaseResult, error) {
	switch size {
	case git.DiffSizeSmall:
		// Small: flat diff path; reviewer count comes from small_diff_reviewers.
		return autoPhaseResult{PhaseStr: domain.SizeSmall}, nil
	case git.DiffSizeLarge:
		fileCount := len(git.ParseDiffSections(diff))
		effectiveGroups := largeDiffReviewers
		if effectiveGroups < minLargeDiffReviewers {
			effectiveGroups = minLargeDiffReviewers
		}
		if effectiveGroups > fileCount {
			effectiveGroups = fileCount
		}
		if effectiveGroups < 2 {
			// Grouped path needs >=2 diff groups to be meaningful. Fall back to
			// flat medium (parsePhases + reviewAgents) without ever touching
			// buildAgents, so per-phase override CLI availability does not gate
			// the fallback.
			return autoPhaseResult{
				PhaseStr:        domain.SizeMedium,
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason: fmt.Sprintf(
					"only %d non-empty diff group(s) possible (file_count=%d, large_diff_reviewers=%d)",
					effectiveGroups, fileCount, largeDiffReviewers),
			}, nil
		}
		// Grouped path confirmed viable; now build the per-phase agents. An
		// error here means the user configured arch_reviewer_agent /
		// diff_reviewer_agents but the CLI is not available — surface it to
		// the caller so the run aborts with a clear message (we intentionally
		// do NOT silently fall back, because the override was explicit).
		archAgent, diffAgents, agentsErr := buildAgents()
		if agentsErr != nil {
			return autoPhaseResult{}, agentsErr
		}
		specs, err := buildGroupedDiffSpecs(diff, guidance, diffPrecomputed, archAgent, diffAgents, effectiveGroups)
		if err != nil {
			return autoPhaseResult{
				PhaseStr:        domain.SizeMedium,
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason:  fmt.Sprintf("buildGroupedDiffSpecs failed: %v", err),
			}, nil
		}
		// specs[0] is always the arch spec; remaining entries are diff groups.
		diffGroupCount := len(specs) - 1
		if diffGroupCount < 2 {
			return autoPhaseResult{
				PhaseStr:        domain.SizeMedium,
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason:  fmt.Sprintf("only %d non-empty diff group(s) after building", diffGroupCount),
			}, nil
		}
		return autoPhaseResult{
			GroupedSpecs: specs,
			UseGrouped:   true,
		}, nil
	default: // medium
		if diff == "" {
			return autoPhaseResult{
				PhaseStr:        domain.SizeMedium,
				MediumDiffCount: mediumDiffReviewers,
			}, nil
		}
		fileCount := len(git.ParseDiffSections(diff))
		effectiveGroups := mediumDiffReviewers
		if effectiveGroups < minMediumDiffReviewers {
			effectiveGroups = minMediumDiffReviewers
		}
		if effectiveGroups > fileCount {
			effectiveGroups = fileCount
		}
		if effectiveGroups < 2 {
			return autoPhaseResult{
				PhaseStr:        domain.SizeMedium,
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason: fmt.Sprintf(
					"only %d non-empty diff group(s) possible (file_count=%d, medium_diff_reviewers=%d)",
					effectiveGroups, fileCount, mediumDiffReviewers),
			}, nil
		}
		archAgent, diffAgents, agentsErr := buildAgents()
		if agentsErr != nil {
			return autoPhaseResult{}, agentsErr
		}
		medSpecs, medErr := buildMediumDiffSpecs(diff, guidance, diffPrecomputed, archAgent, diffAgents, effectiveGroups)
		if medErr != nil {
			return autoPhaseResult{}, medErr
		}
		if medSpecs == nil {
			return autoPhaseResult{
				PhaseStr:        domain.SizeMedium,
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason:  "diff split not viable after grouping",
			}, nil
		}
		return autoPhaseResult{
			GroupedSpecs: medSpecs,
			UseGrouped:   true,
		}, nil
	}
}

// resolvePhaseLarge handles the --phase large explicit override path.
// It builds grouped specs (arch + N diff groups) or falls back to medium.
func resolvePhaseLarge(
	diff, guidance string,
	diffPrecomputed bool,
	buildAgents func() (agent.Agent, []agent.Agent, error),
	largeDiffReviewers int,
	minLargeDiffReviewers int,
	mediumDiffReviewers int,
) (autoPhaseResult, error) {
	sections := git.ParseDiffSections(diff)
	fileCount := len(sections)
	effectiveReviewers := largeDiffReviewers
	if effectiveReviewers < minLargeDiffReviewers {
		effectiveReviewers = minLargeDiffReviewers
	}
	effectiveGroups := effectiveReviewers
	if effectiveGroups > fileCount {
		effectiveGroups = fileCount
	}
	if effectiveGroups < 2 {
		return autoPhaseResult{
			PhaseStr:        domain.SizeMedium,
			MediumDiffCount: mediumDiffReviewers,
			FallbackReason:  fmt.Sprintf("only %d file(s), need >=2 for grouped review", fileCount),
		}, nil
	}
	archAgent, diffAgents, agentsErr := buildAgents()
	if agentsErr != nil {
		return autoPhaseResult{}, agentsErr
	}
	specs, err := buildGroupedDiffSpecs(diff, guidance, diffPrecomputed, archAgent, diffAgents, effectiveGroups)
	if err != nil {
		return autoPhaseResult{
			PhaseStr:        domain.SizeMedium,
			MediumDiffCount: mediumDiffReviewers,
			FallbackReason:  fmt.Sprintf("buildGroupedDiffSpecs failed: %v", err),
		}, nil
	}
	diffGroupCount := len(specs) - 1
	if diffGroupCount < 2 {
		return autoPhaseResult{
			PhaseStr:        domain.SizeMedium,
			MediumDiffCount: mediumDiffReviewers,
			FallbackReason:  fmt.Sprintf("only %d non-empty diff group(s) after building", diffGroupCount),
		}, nil
	}
	return autoPhaseResult{
		GroupedSpecs: specs,
		UseGrouped:   true,
	}, nil
}

// buildPhaseAgents constructs the arch-phase agent and diff-phase agent slice
// used by the auto-phase grouped diff path. It honors per-phase overrides
// (opts.ArchReviewerAgent, opts.DiffReviewerAgents) and falls back to
// opts.ReviewerAgents when an override is unset, preserving pre-Round-9
// behavior. Each constructed agent picks its model/effort via
// modelconfig.ResolveReviewer with the matching phase ("arch" or "diff") so
// arch_reviewer / diff_reviewer model layers continue to apply.
func buildPhaseAgents(opts ReviewOpts, sizeStr string) (agent.Agent, []agent.Agent, error) {
	// Arch agent name: explicit override > first reviewer agent.
	archAgentName := opts.ArchReviewerAgent
	if archAgentName == "" {
		if len(opts.ReviewerAgents) == 0 {
			return nil, nil, fmt.Errorf("reviewer_agents is empty; cannot derive arch reviewer")
		}
		archAgentName = opts.ReviewerAgents[0]
	}
	cliRevModel, legacyRevModel := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
	archSpec := modelconfig.ResolveReviewer(
		opts.Models, sizeStr, domain.PhaseArch, archAgentName,
		cliRevModel, "",
		legacyRevModel, "",
	)
	archAgent, err := agent.NewAgentWithOptions(archAgentName, agentOptions(archSpec.Model, archSpec.Effort, opts))
	if err != nil {
		return nil, nil, fmt.Errorf("arch reviewer %q: %w", archAgentName, err)
	}
	if err := archAgent.IsAvailable(); err != nil {
		return nil, nil, fmt.Errorf("arch reviewer %q unavailable: %w", archAgentName, err)
	}

	// Diff agent names: explicit override > all reviewer agents.
	diffAgentNames := opts.DiffReviewerAgents
	if len(diffAgentNames) == 0 {
		diffAgentNames = opts.ReviewerAgents
	}
	if len(diffAgentNames) == 0 {
		return nil, nil, fmt.Errorf("no diff-phase agents available (reviewer_agents and diff_reviewer_agents both empty)")
	}
	diffAgents := make([]agent.Agent, 0, len(diffAgentNames))
	for _, name := range diffAgentNames {
		spec := modelconfig.ResolveReviewer(
			opts.Models, sizeStr, domain.PhaseDiff, name,
			cliRevModel, "",
			legacyRevModel, "",
		)
		a, err := agent.NewAgentWithOptions(name, agentOptions(spec.Model, spec.Effort, opts))
		if err != nil {
			return nil, nil, fmt.Errorf("diff reviewer %q: %w", name, err)
		}
		if err := a.IsAvailable(); err != nil {
			return nil, nil, fmt.Errorf("diff reviewer %q unavailable: %w", name, err)
		}
		diffAgents = append(diffAgents, a)
	}
	return archAgent, diffAgents, nil
}

// logPerPhaseModelMatrix emits the arch_reviewer[...] and diff_reviewer[...]
// verbose rows that continue the "Effective model matrix" block printed
// earlier. It is only meaningful once resolveAutoPhase has committed to the
// grouped path: a large→flat fallback makes these rows misleading because
// the per-phase override agents are never invoked in that case. Callers must
// gate this on apr.UseGrouped.
func logPerPhaseModelMatrix(logger *terminal.Logger, opts ReviewOpts, sizeStr string) {
	cliRevModelLog, legacyRevModelLog := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
	// Arch: explicit override takes precedence; otherwise fall back to the first
	// reviewer agent. Evaluation order mirrors buildPhaseAgents so that verbose
	// logging never panics when ReviewerAgents is empty but ArchReviewerAgent is
	// set explicitly.
	archName := opts.ArchReviewerAgent
	if archName == "" {
		if len(opts.ReviewerAgents) == 0 {
			// No arch name resolvable. buildPhaseAgents would have already aborted
			// the run with a clear error; logging is defense-in-depth only, so
			// emit a diagnostic line and skip the per-phase rows rather than panic.
			logger.Logf(terminal.StyleWarning, "  arch_reviewer[?]    : (unresolvable: reviewer_agents is empty and arch_reviewer_agent is unset)")
			return
		}
		archName = opts.ReviewerAgents[0]
	}
	archSpec := modelconfig.ResolveReviewer(
		opts.Models, sizeStr, domain.PhaseArch, archName,
		cliRevModelLog, "",
		legacyRevModelLog, "",
	)
	logger.Logf(terminal.StyleDim, "  arch_reviewer[%s]  : %s", archName, formatSpec(agent.AgentOptions{Model: archSpec.Model, Effort: archSpec.Effort}))
	// Diff: explicit override takes precedence; otherwise fall back to all
	// reviewer agents. Empty result is tolerated (for consistency with arch
	// guard above) and emits nothing.
	diffNames := opts.DiffReviewerAgents
	if len(diffNames) == 0 {
		diffNames = opts.ReviewerAgents
	}
	for _, name := range diffNames {
		diffSpec := modelconfig.ResolveReviewer(
			opts.Models, sizeStr, domain.PhaseDiff, name,
			cliRevModelLog, "",
			legacyRevModelLog, "",
		)
		logger.Logf(terminal.StyleDim, "  diff_reviewer[%s]  : %s", name, formatSpec(agent.AgentOptions{Model: diffSpec.Model, Effort: diffSpec.Effort}))
	}
}

// logCrossCheckModelMatrix emits the cross_check[...] verbose row(s) that
// continue the "Effective model matrix" block printed earlier. Like the
// arch_reviewer / diff_reviewer rows, emission is deferred until
// resolveAutoPhase has committed to the grouped path: cross-check fires only
// when useGroupedSpecs=true (Round-14 F#1), so emitting these rows earlier
// would be misleading on small/medium/fallback paths.
//
// Defense-in-depth against panic: if cross-check resolution would fail (every
// selected agent uncovered by the models tree AND no top-level model), this
// function emits a diagnostic line and returns rather than panicking. The
// authoritative resolve happens in the same gate where this is called from
// (cmd/acr/review.go grouped path), so a real failure surfaces there with a
// clear, actionable error.
func logCrossCheckModelMatrix(logger *terminal.Logger, opts ReviewOpts, sizeStr string) {
	ccAgentNames, ccModels, err := resolveCrossCheckAgents(opts, sizeStr)
	if err != nil {
		// Mirror logPerPhaseModelMatrix's posture: skip rows with a hint, do
		// not panic. The same resolve runs in the grouped path gate moments
		// later and will abort the run with the real error.
		logger.Logf(terminal.StyleWarning, "  cross_check[?]    : (unresolvable: %v)", err)
		return
	}
	for i, name := range ccAgentNames {
		cliCCModelLog, legacyCCModelLog := cliOrLegacy(ccModels[i], opts.CrossCheckModelFromCLI)
		perSpec := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RoleCrossCheck, name,
			cliCCModelLog, "",
			legacyCCModelLog, "",
		)
		logger.Logf(terminal.StyleDim, "  cross_check[%s]    : %s", name, formatSpec(agent.AgentOptions{Model: perSpec.Model, Effort: perSpec.Effort}))
	}
}

// buildGroupedDiffSpecs creates ReviewerSpecs for grouped diff review.
// It parses the precomputed diff into sections, groups them, and generates:
//   - 1 arch spec (full diff, GroupKey="arch") using archAgent
//   - N diff specs (per-group filtered diff, GroupKey="g01"..."gNN") with
//     diffAgents assigned round-robin per non-empty diff group
//
// maxDiffGroups is supplied by the caller (resolveAutoPhase) from the
// dedicated `large_diff_reviewers` knob, capped at the actual file count so that
// every group has at least 1 file.
//
// buildDiffSpecsCore builds ReviewerSpecs from pre-grouped diff sections.
// It creates one arch spec (full diff) followed by one diff spec per group.
// When skipEmptyGroups is true, groups whose joined diff is empty are silently
// skipped; otherwise an error is returned.
func buildDiffSpecsCore(
	groups []git.DiffGroup,
	fullDiff, guidance string,
	diffPrecomputed bool,
	archAgent agent.Agent,
	diffAgents []agent.Agent,
	skipEmptyGroups bool,
) ([]runner.ReviewerSpec, error) {
	specs := make([]runner.ReviewerSpec, 0, 1+len(groups))

	specs = append(specs, runner.ReviewerSpec{
		ReviewerID:      1,
		Agent:           archAgent,
		Phase:           domain.PhaseArch,
		GroupKey:        domain.PhaseArch,
		Guidance:        guidance,
		Diff:            fullDiff,
		DiffPrecomputed: diffPrecomputed,
	})

	diffReviewerIdx := 0
	for _, group := range groups {
		groupDiff := git.JoinDiffSections(group.Sections)
		if groupDiff == "" {
			if skipEmptyGroups {
				continue
			}
			return nil, fmt.Errorf("diff split: group %q produced empty diff from %d section(s)", group.Key, len(group.Sections))
		}
		var targetFiles []string
		for _, s := range group.Sections {
			targetFiles = append(targetFiles, s.FilePath)
		}
		reviewerID := len(specs) + 1
		diffReviewerIdx++
		diffAgent := agent.AgentForReviewer(diffAgents, diffReviewerIdx)
		specs = append(specs, runner.ReviewerSpec{
			ReviewerID:      reviewerID,
			Agent:           diffAgent,
			Phase:           domain.PhaseDiff,
			GroupKey:        group.Key,
			Guidance:        guidance,
			Diff:            groupDiff,
			DiffPrecomputed: true,
			TargetFiles:     targetFiles,
		})
	}

	return specs, nil
}

// archAgent and diffAgents are independent so per-phase reviewer overrides
// (arch_reviewer_agent / diff_reviewer_agents) can route phases to different
// agent backends.
func buildGroupedDiffSpecs(
	fullDiff, guidance string,
	diffPrecomputed bool,
	archAgent agent.Agent,
	diffAgents []agent.Agent,
	maxDiffGroups int,
) ([]runner.ReviewerSpec, error) {
	sections := git.ParseDiffSections(fullDiff)
	if len(sections) == 0 {
		return nil, fmt.Errorf("no diff sections found in precomputed diff")
	}
	groups := git.GroupDiffSections(sections, 0, 0, maxDiffGroups)
	if len(groups) == 0 {
		return nil, fmt.Errorf("grouping produced no groups")
	}
	return buildDiffSpecsCore(groups, fullDiff, guidance, diffPrecomputed, archAgent, diffAgents, true)
}

func buildMediumDiffSpecs(
	fullDiff, guidance string,
	diffPrecomputed bool,
	archAgent agent.Agent,
	diffAgents []agent.Agent,
	mediumDiffReviewers int,
) ([]runner.ReviewerSpec, error) {
	if mediumDiffReviewers < 2 {
		return nil, nil
	}
	sections := git.ParseDiffSections(fullDiff)
	if len(sections) < 2 {
		return nil, nil
	}
	maxFilesPerGroup := (len(sections) + mediumDiffReviewers - 1) / mediumDiffReviewers
	groups := git.GroupDiffSections(sections, maxFilesPerGroup, 0, mediumDiffReviewers)
	if len(groups) < 2 {
		return nil, nil
	}
	return buildDiffSpecsCore(groups, fullDiff, guidance, diffPrecomputed, archAgent, diffAgents, false)
}

// diffFindingGroups returns groups present in before but not in after.
// Relies on filter.Apply preserving order, so after is an ordered subsequence.
func diffFindingGroups(before, after []domain.FindingGroup) []domain.FindingGroup {
	j := 0
	var removed []domain.FindingGroup
	for i := range before {
		if j < len(after) && slices.Equal(before[i].Sources, after[j].Sources) {
			j++
		} else {
			removed = append(removed, before[i])
		}
	}
	return removed
}

// formatSpec formats a model/effort pair for the verbose effective matrix log.
// Empty fields are shown as "(default)" to distinguish "not set" from a blank value.
func formatSpec(opts agent.AgentOptions) string {
	model := opts.Model
	if model == "" {
		model = "(default)"
	}
	effort := opts.Effort
	if effort == "" {
		effort = "(default)"
	}
	return fmt.Sprintf("%s [%s]", model, effort)
}

// formatSizeStr returns the size string for display, substituting "(unknown)"
// when classification was not available (empty string).
func formatSizeStr(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
