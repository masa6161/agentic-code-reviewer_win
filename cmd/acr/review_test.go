package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestParsePhases(t *testing.T) {
	tests := []struct {
		name      string
		phaseStr  string
		reviewers int
		want      []runner.PhaseConfig
		wantErr   bool
	}{
		{
			name:      "small with 1 reviewer",
			phaseStr:  "small",
			reviewers: 1,
			want:      []runner.PhaseConfig{{Phase: "diff", ReviewerCount: 1}},
		},
		{
			name:      "small with 3 reviewers",
			phaseStr:  "small",
			reviewers: 3,
			want:      []runner.PhaseConfig{{Phase: "diff", ReviewerCount: 3}},
		},
		{
			name:      "medium with 3 reviewers",
			phaseStr:  "medium",
			reviewers: 3,
			want: []runner.PhaseConfig{
				{Phase: "arch", ReviewerCount: 1},
				{Phase: "diff", ReviewerCount: 2},
			},
		},
		{
			name:      "medium with 2 reviewers (minimum)",
			phaseStr:  "medium",
			reviewers: 2,
			want: []runner.PhaseConfig{
				{Phase: "arch", ReviewerCount: 1},
				{Phase: "diff", ReviewerCount: 1},
			},
		},
		{
			name:      "medium with 1 reviewer errors (needs >= 2)",
			phaseStr:  "medium",
			reviewers: 1,
			wantErr:   true,
		},
		{
			name:      "unknown phase",
			phaseStr:  "unknown",
			reviewers: 3,
			wantErr:   true,
		},
		{
			name:      "old phase name diff is rejected",
			phaseStr:  "diff",
			reviewers: 3,
			wantErr:   true,
		},
		{
			name:      "old phase name arch is rejected",
			phaseStr:  "arch",
			reviewers: 3,
			wantErr:   true,
		},
		{
			name:      "zero reviewers",
			phaseStr:  "small",
			reviewers: 0,
			wantErr:   true,
		},
		{
			name:      "negative reviewers",
			phaseStr:  "small",
			reviewers: -1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePhases(tt.phaseStr, tt.reviewers)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result: %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d phases, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Phase != tt.want[i].Phase {
					t.Errorf("phase[%d].Phase = %q, want %q", i, got[i].Phase, tt.want[i].Phase)
				}
				if got[i].ReviewerCount != tt.want[i].ReviewerCount {
					t.Errorf("phase[%d].ReviewerCount = %d, want %d", i, got[i].ReviewerCount, tt.want[i].ReviewerCount)
				}
			}
		})
	}
}

// --- buildGroupedDiffSpecs tests ---

// makeSectionsForReview creates n DiffSections with specified added lines per file.
func makeSectionsForReview(n, addedLines int) []git.DiffSection {
	sections := make([]git.DiffSection, n)
	for i := range sections {
		name := fmt.Sprintf("file%d.go", i+1)
		sections[i] = git.DiffSection{
			FilePath:   name,
			Content:    fmt.Sprintf("diff --git a/%s b/%s\n+line", name, name),
			AddedLines: addedLines,
		}
	}
	return sections
}

func TestBuildGroupedDiffSpecs_BasicGroups(t *testing.T) {
	// 9 files × 10 lines → with default 5 files/group → 2 groups + 1 arch = 3 specs
	sections := makeSectionsForReview(9, 10)
	fullDiff := git.JoinDiffSections(sections)

	agents := []agent.Agent{agent.NewCodexAgent("")}
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents[0], agents, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1 arch + at least 2 diff groups
	if len(specs) < 3 {
		t.Fatalf("expected at least 3 specs (1 arch + 2+ diff), got %d", len(specs))
	}

	// First spec is arch
	if specs[0].Phase != "arch" {
		t.Errorf("specs[0].Phase = %q, want %q", specs[0].Phase, "arch")
	}
	if specs[0].GroupKey != "arch" {
		t.Errorf("specs[0].GroupKey = %q, want %q", specs[0].GroupKey, "arch")
	}
}

func TestBuildGroupedDiffSpecs_GroupKeysAssigned(t *testing.T) {
	sections := makeSectionsForReview(6, 10)
	fullDiff := git.JoinDiffSections(sections)

	agents := []agent.Agent{agent.NewCodexAgent("")}
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents[0], agents, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if specs[0].GroupKey != "arch" {
		t.Errorf("specs[0].GroupKey = %q, want %q", specs[0].GroupKey, "arch")
	}
	for i := 1; i < len(specs); i++ {
		if specs[i].Phase != "diff" {
			t.Errorf("specs[%d].Phase = %q, want %q", i, specs[i].Phase, "diff")
		}
		if specs[i].GroupKey == "" {
			t.Errorf("specs[%d].GroupKey is empty", i)
		}
	}
}

func TestBuildGroupedDiffSpecs_TargetFilesSet(t *testing.T) {
	sections := makeSectionsForReview(3, 10)
	fullDiff := git.JoinDiffSections(sections)

	agents := []agent.Agent{agent.NewCodexAgent("")}
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents[0], agents, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Arch spec should not have TargetFiles
	if len(specs[0].TargetFiles) != 0 {
		t.Errorf("arch spec should not have TargetFiles, got %v", specs[0].TargetFiles)
	}

	// Diff specs should have TargetFiles
	for i := 1; i < len(specs); i++ {
		if len(specs[i].TargetFiles) == 0 {
			t.Errorf("specs[%d].TargetFiles is empty", i)
		}
	}
}

func TestBuildGroupedDiffSpecs_RespectsMaxGroups(t *testing.T) {
	// 20 files → normally many groups, but maxDiffGroups=2
	sections := makeSectionsForReview(20, 10)
	fullDiff := git.JoinDiffSections(sections)

	agents := []agent.Agent{agent.NewCodexAgent("")}
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents[0], agents, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1 arch + max 2 diff = max 3 specs
	if len(specs) > 3 {
		t.Errorf("expected at most 3 specs (1 arch + 2 diff), got %d", len(specs))
	}
}

func TestBuildGroupedDiffSpecs_SnapshotConsistency(t *testing.T) {
	sections := makeSectionsForReview(6, 10)
	fullDiff := git.JoinDiffSections(sections)

	agents := []agent.Agent{agent.NewCodexAgent("")}
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents[0], agents, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Arch spec should have full diff
	if specs[0].Diff != fullDiff {
		t.Errorf("arch spec diff should equal full diff")
	}

	// Every diff spec's target files should appear in the full diff
	for i := 1; i < len(specs); i++ {
		for _, tf := range specs[i].TargetFiles {
			if !strings.Contains(fullDiff, tf) {
				t.Errorf("specs[%d] target file %q not found in full diff", i, tf)
			}
		}
	}
}

func TestBuildGroupedDiffSpecs_FallbackOnError(t *testing.T) {
	agents := []agent.Agent{agent.NewCodexAgent("")}
	_, err := buildGroupedDiffSpecs("", "", true, agents[0], agents, 4)
	if err == nil {
		t.Fatal("expected error for empty diff, got nil")
	}
}

// --- resolveAutoPhase tests ---
//
// Signature: resolveAutoPhase(size, diff, guidance, diffPrecomputed,
//   buildAgents func() (agent.Agent, []agent.Agent, error),
//   largeDiffGroups, mediumDiffReviewers) (autoPhaseResult, error)
//
// Tests use testBuildAgents to return pre-constructed agents synchronously.
// The closure is only invoked when the large grouped path decides to proceed,
// so medium/small/fallback tests can pass nil values safely.

func testBuildAgents(arch agent.Agent, diffs []agent.Agent) func() (agent.Agent, []agent.Agent, error) {
	return func() (agent.Agent, []agent.Agent, error) {
		return arch, diffs, nil
	}
}

func TestAutoPhase_Large_GroupedSpecs(t *testing.T) {
	// Large diff with large_diff_groups=3 → grouped specs
	sections := makeSectionsForReview(12, 10)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr, _ := resolveAutoPhase(git.DiffSizeLarge, fullDiff, "", true, testBuildAgents(agents[0], agents), 3, 2)

	if !apr.UseGrouped {
		t.Fatal("expected UseGrouped=true for large diff with large_diff_groups=3")
	}
	if apr.PhaseStr != "" {
		t.Errorf("expected empty PhaseStr, got %q", apr.PhaseStr)
	}
	if len(apr.GroupedSpecs) < 2 {
		t.Errorf("expected at least 2 specs (1 arch + 1+ diff), got %d", len(apr.GroupedSpecs))
	}
}

func TestAutoPhase_Large_FallbackOnError(t *testing.T) {
	// Large diff but empty diff content → buildGroupedDiffSpecs fails → fallback
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr, _ := resolveAutoPhase(git.DiffSizeLarge, "", "", true, testBuildAgents(agents[0], agents), 3, 2)

	if apr.UseGrouped {
		t.Fatal("expected UseGrouped=false when buildGroupedDiffSpecs fails")
	}
	if apr.PhaseStr != "medium" {
		t.Errorf("expected PhaseStr 'medium', got %q", apr.PhaseStr)
	}
	if apr.MediumDiffCount != 2 {
		t.Errorf("expected MediumDiffCount=2 (from medium_diff_reviewers), got %d", apr.MediumDiffCount)
	}
}

// --- buildCrossCheckContext tests ---

func TestBuildCrossCheckContext_GroupTopology(t *testing.T) {
	specs := []runner.ReviewerSpec{
		{ReviewerID: 1, Phase: "arch", GroupKey: "arch"},
		{ReviewerID: 2, Phase: "diff", GroupKey: "g01", TargetFiles: []string{"a.go"}},
		{ReviewerID: 3, Phase: "diff", GroupKey: "g02", TargetFiles: []string{"b.go"}},
	}
	rawFindings := []domain.Finding{
		{Text: "issue", ReviewerID: 2, GroupKey: "g01"},
	}
	aggregated := domain.AggregateFindings(rawFindings)
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Findings: []domain.Finding{}},
		{ReviewerID: 2, ExitCode: 0, Findings: rawFindings},
		{ReviewerID: 3, ExitCode: 1, TimedOut: true},
	}

	ccCtx := buildCrossCheckContext(aggregated, specs, results)

	if len(ccCtx.Groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(ccCtx.Groups))
	}
	if ccCtx.Groups[0].GroupKey != "arch" || !ccCtx.Groups[0].FullDiff {
		t.Errorf("expected arch group with FullDiff=true, got %+v", ccCtx.Groups[0])
	}
	if ccCtx.Groups[1].GroupKey != "g01" || ccCtx.Groups[1].FullDiff {
		t.Errorf("expected g01 with FullDiff=false, got %+v", ccCtx.Groups[1])
	}
	if len(ccCtx.Outcomes) != 3 {
		t.Fatalf("expected 3 outcomes, got %d", len(ccCtx.Outcomes))
	}
	// g01 succeeded
	if !ccCtx.Outcomes[1].Succeeded || ccCtx.Outcomes[1].FindingCount != 1 {
		t.Errorf("g01 outcome wrong: %+v", ccCtx.Outcomes[1])
	}
	// g02 timed out
	if !ccCtx.Outcomes[2].TimedOut || ccCtx.Outcomes[2].Succeeded {
		t.Errorf("g02 outcome wrong: %+v", ccCtx.Outcomes[2])
	}
}

func TestBuildCrossCheckContext_DedupGroupKey(t *testing.T) {
	specs := []runner.ReviewerSpec{
		{Phase: "arch", GroupKey: "arch"},
		{Phase: "arch", GroupKey: "arch"}, // duplicate key
	}
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0},
		{ReviewerID: 2, ExitCode: 0},
	}
	ccCtx := buildCrossCheckContext(nil, specs, results)
	if len(ccCtx.Groups) != 1 {
		t.Errorf("expected 1 unique group, got %d", len(ccCtx.Groups))
	}
}

// TestReviewPipeline_CrossCheckUsesAggregatedIDs verifies that when two raw
// reviewer findings share the same text, AggregateFindings collapses them to
// one entry, and that aggregated slice (not the raw slice) is what reaches the
// cross-check context. The payload must contain exactly one finding with ID 0,
// not two findings with IDs 0 and 1.
func TestReviewPipeline_CrossCheckUsesAggregatedIDs(t *testing.T) {
	// Two reviewers report the same text — one from each diff group.
	raw := []domain.Finding{
		{Text: "missing error check", ReviewerID: 1, GroupKey: "g01"},
		{Text: "missing error check", ReviewerID: 2, GroupKey: "g02"},
	}
	aggregated := domain.AggregateFindings(raw)
	if len(aggregated) != 1 {
		t.Fatalf("precondition: expected 1 aggregated finding, got %d", len(aggregated))
	}

	specs := []runner.ReviewerSpec{
		{Phase: "arch", GroupKey: "arch"},
		{Phase: "diff", GroupKey: "g01", TargetFiles: []string{"a.go"}},
		{Phase: "diff", GroupKey: "g02", TargetFiles: []string{"b.go"}},
	}
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Findings: []domain.Finding{raw[0]}},
		{ReviewerID: 2, ExitCode: 0, Findings: []domain.Finding{raw[1]}},
	}

	// Pipeline passes aggregated (not raw) to buildCrossCheckContext.
	ccCtx := buildCrossCheckContext(aggregated, specs, results)

	if len(ccCtx.Findings) != 1 {
		t.Fatalf("expected 1 finding in cross-check context, got %d (passing raw would give 2)", len(ccCtx.Findings))
	}

	// The summarizer package is internal, but we can verify the ID space
	// by checking the finding text matches the aggregated entry.
	if ccCtx.Findings[0].Text != "missing error check" {
		t.Errorf("unexpected finding text %q", ccCtx.Findings[0].Text)
	}
}

// --- resolveCrossCheckAgents tests ---

func TestResolveCrossCheckAgents_DisabledReturnsNil(t *testing.T) {
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{CrossCheckEnabled: false}}
	names, models, err := resolveCrossCheckAgents(opts, "")
	if err != nil {
		t.Fatalf("disabled: unexpected error: %v", err)
	}
	if names != nil || models != nil {
		t.Errorf("disabled: expected nil names/models, got names=%v models=%v", names, models)
	}
}

func TestResolveCrossCheckAgents_RequiresModel(t *testing.T) {
	// Top-level CrossCheckModel empty AND models tree empty: every selected
	// agent fails per-agent resolution → error must name the missing agent(s)
	// so users know which slot to fill.
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex,claude",
		CrossCheckModel:   "",
	}}
	_, _, err := resolveCrossCheckAgents(opts, "")
	if err == nil {
		t.Fatal("expected error when CrossCheckModel is empty and models tree is empty")
	}
	if !strings.Contains(err.Error(), "requires cross_check.model") {
		t.Errorf("error should mention cross_check.model requirement, got: %v", err)
	}
	if !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "claude") {
		t.Errorf("error should name both missing agents, got: %v", err)
	}
}

// --- Round-13 F#1: per-agent fallback via models tree ---

func TestResolveCrossCheckAgents_ResolvesFromAgentsModelsTree(t *testing.T) {
	// Top-level CrossCheckModel empty but agents.<name>.cross_check.model set
	// for every selected agent → success. This is the canonical "structured
	// models tree instead of legacy flat field" configuration.
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex,claude",
		CrossCheckModel:   "",
		Models: config.ModelsConfig{
			Agents: map[string]config.RoleModels{
				"codex":  {CrossCheck: &config.ModelSpec{Model: "gpt-5.4-mini"}},
				"claude": {CrossCheck: &config.ModelSpec{Model: "claude-opus-4-7"}},
			},
		},
	}}
	names, models, err := resolveCrossCheckAgents(opts, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 || len(models) != 2 {
		t.Fatalf("expected 2 agents and 2 models, got names=%v models=%v", names, models)
	}
	if models[0] != "gpt-5.4-mini" || models[1] != "claude-opus-4-7" {
		t.Errorf("expected models from agents tree, got %v", models)
	}
}

func TestResolveCrossCheckAgents_ResolvesFromDefaultsModelsTree(t *testing.T) {
	// Top-level empty + only defaults.cross_check.model set → all agents get
	// the default model.
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex,claude",
		CrossCheckModel:   "",
		Models: config.ModelsConfig{
			Defaults: config.RoleModels{CrossCheck: &config.ModelSpec{Model: "shared-cc-model"}},
		},
	}}
	_, models, err := resolveCrossCheckAgents(opts, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 || models[0] != "shared-cc-model" || models[1] != "shared-cc-model" {
		t.Errorf("expected both agents to resolve to shared default, got %v", models)
	}
}

func TestResolveCrossCheckAgents_ResolvesFromSizesModelsTree(t *testing.T) {
	// Top-level empty + sizes.large.cross_check.model only → resolves when
	// sizeStr matches (this exercises the runtime size-layer fallback path
	// that validate-time cannot verify deterministically).
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex",
		CrossCheckModel:   "",
		Models: config.ModelsConfig{
			Sizes: map[string]config.RoleModels{
				"large": {CrossCheck: &config.ModelSpec{Model: "gpt-5.4-large"}},
			},
		},
	}}
	_, models, err := resolveCrossCheckAgents(opts, "large")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 || models[0] != "gpt-5.4-large" {
		t.Errorf("expected model from sizes tree at size=large, got %v", models)
	}
}

// TestResolveCrossCheckAgents_PartialTreeReportsMissingAgent verifies the
// Round-13 F#3 tightening: when the models tree covers some selected agents
// but not all, runtime must name the uncovered agent(s) rather than silently
// succeeding for the covered subset.
func TestResolveCrossCheckAgents_PartialTreeReportsMissingAgent(t *testing.T) {
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex,claude",
		CrossCheckModel:   "",
		Models: config.ModelsConfig{
			Agents: map[string]config.RoleModels{
				"codex": {CrossCheck: &config.ModelSpec{Model: "gpt-5.4-mini"}},
				// "claude" intentionally absent and no defaults fallback.
			},
		},
	}}
	_, _, err := resolveCrossCheckAgents(opts, "")
	if err == nil {
		t.Fatal("expected error when one selected agent is uncovered by the models tree")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error should name the uncovered agent 'claude', got: %v", err)
	}
	if strings.Contains(err.Error(), "[codex]") {
		t.Errorf("error should NOT name the covered agent 'codex', got: %v", err)
	}
}

func TestResolveCrossCheckAgents_PairsAgentsAndModels(t *testing.T) {
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex,claude,gemini",
		CrossCheckModel:   "gpt-5,opus,gemini-pro",
	}}
	names, models, err := resolveCrossCheckAgents(opts, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 3 || len(models) != 3 {
		t.Fatalf("expected 3 agents and 3 models, got names=%v models=%v", names, models)
	}
	if names[0] != "codex" || models[0] != "gpt-5" {
		t.Errorf("expected pair[0]=(codex,gpt-5), got (%s,%s)", names[0], models[0])
	}
	if names[1] != "claude" || models[1] != "opus" {
		t.Errorf("expected pair[1]=(claude,opus), got (%s,%s)", names[1], models[1])
	}
	if names[2] != "gemini" || models[2] != "gemini-pro" {
		t.Errorf("expected pair[2]=(gemini,gemini-pro), got (%s,%s)", names[2], models[2])
	}
}

func TestResolveCrossCheckAgents_RejectsCountMismatch(t *testing.T) {
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex,claude,gemini",
		CrossCheckModel:   "gpt-5,opus",
	}}
	_, _, err := resolveCrossCheckAgents(opts, "")
	if err == nil {
		t.Fatal("expected error when agent count != model count")
	}
	if !strings.Contains(err.Error(), "same count") {
		t.Errorf("error should mention count mismatch, got: %v", err)
	}
}

func TestResolveCrossCheckAgents_RejectsEmptyEntry(t *testing.T) {
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "codex,claude",
		CrossCheckModel:   "gpt-5,",
	}}
	_, _, err := resolveCrossCheckAgents(opts, "")
	if err == nil {
		t.Fatal("expected error when CrossCheckModel has empty entry")
	}
}

func TestResolveCrossCheckAgents_AgentDefaultsToSummarizer(t *testing.T) {
	opts := ReviewOpts{ResolvedConfig: config.ResolvedConfig{
		CrossCheckEnabled: true,
		CrossCheckAgent:   "",
		SummarizerAgent:   "codex",
		CrossCheckModel:   "gpt-5",
	}}
	names, models, err := resolveCrossCheckAgents(opts, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 1 || names[0] != "codex" {
		t.Errorf("expected default to codex (summarizer agent), got %v", names)
	}
	if len(models) != 1 || models[0] != "gpt-5" {
		t.Errorf("expected single model, got %v", models)
	}
}

// --- resolveAutoPhase fallback-on-few-groups tests ---

// TestResolveAutoPhase_OneDiffGroup_FallsBackToFlat verifies that exactly
// 1 non-empty diff group (< 2) still triggers the fallback. With
// large_diff_groups=1 the file_count cap also kicks in early.
func TestResolveAutoPhase_OneDiffGroup_FallsBackToFlat(t *testing.T) {
	// 2 files, large_diff_groups=1 → file_count cap → effectiveGroups=1 → fallback.
	sections := makeSectionsForReview(2, 10)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr, _ := resolveAutoPhase(git.DiffSizeLarge, fullDiff, "", true, testBuildAgents(agents[0], agents), 1, 2)

	if apr.UseGrouped {
		t.Fatal("expected UseGrouped=false for large_diff_groups=1")
	}
	if apr.PhaseStr != "medium" {
		t.Errorf("expected PhaseStr 'medium', got %q", apr.PhaseStr)
	}
	if apr.FallbackReason == "" {
		t.Error("expected non-empty FallbackReason")
	}
	if apr.MediumDiffCount != 2 {
		t.Errorf("expected MediumDiffCount=2 from medium_diff_reviewers, got %d", apr.MediumDiffCount)
	}
}

// TestResolveAutoPhase_TwoLargeDiffGroups_KeepsGrouped verifies that 2 or more
// non-empty diff groups result in UseGrouped=true (no fallback).
func TestResolveAutoPhase_TwoLargeDiffGroups_KeepsGrouped(t *testing.T) {
	// 6 files, large_diff_groups=2 → expect 2 diff groups.
	sections := makeSectionsForReview(6, 10)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr, _ := resolveAutoPhase(git.DiffSizeLarge, fullDiff, "", true, testBuildAgents(agents[0], agents), 2, 2)

	if !apr.UseGrouped {
		t.Fatal("expected UseGrouped=true for 2+ diff groups")
	}
	diffGroupCount := len(apr.GroupedSpecs) - 1
	if diffGroupCount < 2 {
		t.Errorf("expected >= 2 diff groups, got %d", diffGroupCount)
	}
	if apr.FallbackReason != "" {
		t.Errorf("expected empty FallbackReason, got %q", apr.FallbackReason)
	}
}

// Sanity test that grouped diff path produces specs whose GroupKey values
// match the buildCrossCheckContext expected topology (1 arch + N diff groups).
func TestLargeReview_GroupedSpecsProduceCrossCheckableTopology(t *testing.T) {
	sections := makeSectionsForReview(8, 5)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}
	apr, _ := resolveAutoPhase(git.DiffSizeLarge, fullDiff, "", true, testBuildAgents(agents[0], agents), 3, 2)
	if !apr.UseGrouped {
		t.Fatal("expected UseGrouped=true for large diff with large_diff_groups=3")
	}
	if apr.GroupedSpecs[0].GroupKey != "arch" {
		t.Errorf("expected first spec GroupKey=arch, got %q", apr.GroupedSpecs[0].GroupKey)
	}
	seen := map[string]bool{}
	for i, s := range apr.GroupedSpecs {
		if seen[s.GroupKey] {
			t.Errorf("duplicate GroupKey %q at spec %d", s.GroupKey, i)
		}
		seen[s.GroupKey] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected >=2 groups for cross-check, got %d", len(seen))
	}
}

// TestBuildCrossCheckContext_UsesSpecReviewerID verifies that buildCrossCheckContext
// uses spec.ReviewerID (not slice position) to map reviewer results to group keys.
// Specs are given non-sequential IDs and placed in shuffled order; outcomes must
// still be tied to the correct groups regardless of spec slice ordering.
func TestBuildCrossCheckContext_UsesSpecReviewerID(t *testing.T) {
	// Specs in shuffled order with non-sequential explicit ReviewerIDs.
	// ID 7 → "g02", ID 3 → "arch", ID 5 → "g01"
	specs := []runner.ReviewerSpec{
		{ReviewerID: 7, Phase: "diff", GroupKey: "g02", TargetFiles: []string{"b.go"}},
		{ReviewerID: 3, Phase: "arch", GroupKey: "arch"},
		{ReviewerID: 5, Phase: "diff", GroupKey: "g01", TargetFiles: []string{"a.go"}},
	}
	rawFindings := []domain.Finding{
		{Text: "arch issue", ReviewerID: 3, GroupKey: "arch"},
		{Text: "g01 issue", ReviewerID: 5, GroupKey: "g01"},
	}
	aggregated := domain.AggregateFindings(rawFindings)
	results := []domain.ReviewerResult{
		{ReviewerID: 3, ExitCode: 0, Findings: []domain.Finding{rawFindings[0]}}, // arch succeeded
		{ReviewerID: 5, ExitCode: 0, Findings: []domain.Finding{rawFindings[1]}}, // g01 succeeded
		{ReviewerID: 7, ExitCode: -1, TimedOut: true},                            // g02 timed out
	}

	ccCtx := buildCrossCheckContext(aggregated, specs, results)

	// Groups should reflect the order specs appear (shuffled: g02, arch, g01).
	if len(ccCtx.Groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(ccCtx.Groups))
	}
	if len(ccCtx.Outcomes) != 3 {
		t.Fatalf("expected 3 outcomes, got %d", len(ccCtx.Outcomes))
	}

	// Build outcome map by group key for position-independent assertions.
	outcomeByKey := make(map[string]int, 3)
	for i, g := range ccCtx.Groups {
		outcomeByKey[g.GroupKey] = i
	}

	archIdx, ok := outcomeByKey["arch"]
	if !ok {
		t.Fatal("arch group missing from cross-check context")
	}
	if !ccCtx.Outcomes[archIdx].Succeeded || ccCtx.Outcomes[archIdx].FindingCount != 1 {
		t.Errorf("arch outcome wrong (ID=3 must map to arch): %+v", ccCtx.Outcomes[archIdx])
	}

	g01Idx, ok := outcomeByKey["g01"]
	if !ok {
		t.Fatal("g01 group missing from cross-check context")
	}
	if !ccCtx.Outcomes[g01Idx].Succeeded || ccCtx.Outcomes[g01Idx].FindingCount != 1 {
		t.Errorf("g01 outcome wrong (ID=5 must map to g01): %+v", ccCtx.Outcomes[g01Idx])
	}

	g02Idx, ok := outcomeByKey["g02"]
	if !ok {
		t.Fatal("g02 group missing from cross-check context")
	}
	if !ccCtx.Outcomes[g02Idx].TimedOut || ccCtx.Outcomes[g02Idx].Succeeded {
		t.Errorf("g02 outcome wrong (ID=7 must map to g02): %+v", ccCtx.Outcomes[g02Idx])
	}
}

// --- isLGTM gate tests ---

// TestReviewGate_CrossCheckOnlyBlocking_FailsGate: grouped has no findings but
// cross-check reports a blocking finding → gate must return false (not-LGTM).
func TestReviewGate_CrossCheckOnlyBlocking_FailsGate(t *testing.T) {
	grouped := domain.GroupedFindings{} // empty — no grouped findings
	cc := &summarizer.CrossCheckResult{
		Findings: []summarizer.CrossCheckFinding{
			{Title: "critical gap", Severity: "blocking"},
		},
	}
	if isLGTM(grouped, cc) {
		t.Error("expected isLGTM=false when cross-check has blocking finding, got true")
	}
}

// TestReviewGate_NoFindings_AllAdvisory_StillLGTM: grouped empty + cross-check
// has only advisory findings → gate should return true (LGTM).
func TestReviewGate_NoFindings_AllAdvisory_StillLGTM(t *testing.T) {
	grouped := domain.GroupedFindings{}
	cc := &summarizer.CrossCheckResult{
		Findings: []summarizer.CrossCheckFinding{
			{Title: "style note", Severity: "advisory"},
		},
	}
	if !isLGTM(grouped, cc) {
		t.Error("expected isLGTM=true when cross-check findings are advisory only, got false")
	}
}

// TestReviewGate_GroupedFindingsOnly_NotLGTM: grouped has findings (no cc) →
// gate must return false (existing behavior preserved).
func TestReviewGate_GroupedFindingsOnly_NotLGTM(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "nil check missing", Sources: []int{0}},
		},
	}
	if isLGTM(grouped, nil) {
		t.Error("expected isLGTM=false when grouped has findings, got true")
	}
}

// TestReviewGate_BothEmpty_LGTM: grouped empty + ccResult nil → LGTM.
func TestReviewGate_BothEmpty_LGTM(t *testing.T) {
	grouped := domain.GroupedFindings{}
	if !isLGTM(grouped, nil) {
		t.Error("expected isLGTM=true when grouped is empty and ccResult is nil, got false")
	}
}

// --- shouldUseAutoPhase tests ---

// TestShouldUseAutoPhase_NoArgs_UsesAutoPhasePath: default opts (AutoPhase=true,
// Phase="") → auto-phase path taken.
func TestExecuteReview_NoArgs_UsesAutoPhasePath(t *testing.T) {
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{AutoPhase: true},
		Phase:          "",
	}
	if !shouldUseAutoPhase(opts) {
		t.Error("expected shouldUseAutoPhase=true when AutoPhase=true and Phase is empty")
	}
}

// TestExecuteReview_PhaseSmall_UsesFlatPath: explicit --phase small → auto-phase
// is ignored regardless of AutoPhase value.
func TestExecuteReview_PhaseSmall_UsesFlatPath(t *testing.T) {
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{AutoPhase: true},
		Phase:          "small",
	}
	if shouldUseAutoPhase(opts) {
		t.Error("expected shouldUseAutoPhase=false when Phase is explicitly set")
	}
}

// TestExecuteReview_NoAutoPhase_UsesFlatPath: --no-auto-phase (AutoPhase=false,
// Phase="") → flat diff path taken.
func TestExecuteReview_NoAutoPhase_UsesFlatPath(t *testing.T) {
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{AutoPhase: false},
		Phase:          "",
	}
	if shouldUseAutoPhase(opts) {
		t.Error("expected shouldUseAutoPhase=false when AutoPhase=false")
	}
}

// --- applyVerdictExitPolicy tests (Part C exit-code policy) ---

func TestReview_ExitCode_VerdictOk_ExitsZero(t *testing.T) {
	// "ok" verdict is handled via handleLGTM path; applyVerdictExitPolicy only
	// runs on non-ok. But a harmless passthrough of ExitNoFindings should remain
	// ExitNoFindings for defensive callers.
	got := applyVerdictExitPolicy("ok", false, domain.ExitNoFindings)
	if got != domain.ExitNoFindings {
		t.Errorf("expected exit 0, got %d", got)
	}
}

func TestReview_ExitCode_VerdictAdvisory_ExitsZero(t *testing.T) {
	got := applyVerdictExitPolicy("advisory", false, domain.ExitFindings)
	if got != domain.ExitNoFindings {
		t.Errorf("expected advisory to promote to exit 0, got %d", got)
	}
}

func TestReview_ExitCode_VerdictBlocking_ExitsOne(t *testing.T) {
	got := applyVerdictExitPolicy("blocking", false, domain.ExitFindings)
	if got != domain.ExitFindings {
		t.Errorf("expected blocking exit 1, got %d", got)
	}
}

func TestReview_Strict_AdvisoryExitsOne(t *testing.T) {
	got := applyVerdictExitPolicy("advisory", true, domain.ExitFindings)
	if got != domain.ExitFindings {
		t.Errorf("expected advisory+strict exit 1, got %d", got)
	}
}

// TestReview_CrossCheckBlockingPromotesVerdict: when grouped findings are all
// advisory but cross-check reports blocking, ComputeVerdict must promote the
// overall verdict to "blocking" and applyVerdictExitPolicy must return 1.
func TestReview_CrossCheckBlockingPromotesVerdict(t *testing.T) {
	g := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "style", Severity: "advisory"},
		},
	}
	g.ComputeVerdict(true, false) // ccBlocking=true
	if g.Verdict != "blocking" {
		t.Fatalf("expected verdict=blocking when cc has blocking, got %q", g.Verdict)
	}
	got := applyVerdictExitPolicy(g.Verdict, false, domain.ExitFindings)
	if got != domain.ExitFindings {
		t.Errorf("expected exit 1 for promoted blocking, got %d", got)
	}
}

// TestReview_ExitCodePolicy_PropagatesErrors: non-findings exit codes (error,
// interrupted) are passed through untouched by the policy even on advisory.
func TestReview_ExitCodePolicy_PropagatesErrors(t *testing.T) {
	if got := applyVerdictExitPolicy("advisory", false, domain.ExitError); got != domain.ExitError {
		t.Errorf("expected ExitError passthrough, got %d", got)
	}
	if got := applyVerdictExitPolicy("advisory", false, domain.ExitInterrupted); got != domain.ExitInterrupted {
		t.Errorf("expected ExitInterrupted passthrough, got %d", got)
	}
}

// --- Flat-path verdict pipeline tests (Part D) ---

// TestFlatPath_VerdictRendered verifies that on the flat path (AutoPhase=false,
// Phase="small") ComputeVerdict produces the correct verdict for each severity
// class, and that RenderReport + RenderJSON both include the verdict string.
// This is a seam-level test — no subprocess is spawned.
func TestFlatPath_VerdictRendered(t *testing.T) {
	tests := []struct {
		name           string
		findings       []domain.FindingGroup
		wantVerdict    string
		wantExitPolicy domain.ExitCode // result of applyVerdictExitPolicy(verdict, false, ExitFindings)
	}{
		{
			name:           "ok",
			findings:       nil,
			wantVerdict:    "ok",
			wantExitPolicy: domain.ExitNoFindings,
		},
		{
			name: "advisory",
			findings: []domain.FindingGroup{
				{Title: "style note", Severity: "advisory", Sources: []int{0}},
			},
			wantVerdict:    "advisory",
			wantExitPolicy: domain.ExitNoFindings, // advisory + non-strict → 0
		},
		{
			name: "blocking",
			findings: []domain.FindingGroup{
				{Title: "nil deref", Severity: "blocking", Sources: []int{0}},
			},
			wantVerdict:    "blocking",
			wantExitPolicy: domain.ExitFindings, // blocking → 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build a GroupedFindings as the flat path would produce (no cross-check).
			g := domain.GroupedFindings{Findings: tt.findings}
			g.ComputeVerdict(false, false)

			if g.Verdict != tt.wantVerdict {
				t.Fatalf("ComputeVerdict: got %q, want %q", g.Verdict, tt.wantVerdict)
			}

			// RenderReport — must contain "Verdict: <verdict>".
			summaryResult := &summarizer.Result{Grouped: g}
			report := runner.RenderReport(g, summaryResult, domain.ReviewStats{
				TotalReviewers:     1,
				ReviewerDurations:  map[int]time.Duration{},
				ReviewerAgentNames: map[int]string{},
			}, nil)
			wantLine := "Verdict: " + tt.wantVerdict
			if !strings.Contains(report, wantLine) {
				t.Errorf("RenderReport output missing %q\ngot:\n%s", wantLine, report)
			}

			// RenderJSON — must contain "verdict": "<verdict>".
			jsonBytes, err := runner.RenderJSON(&g, nil)
			if err != nil {
				t.Fatalf("RenderJSON error: %v", err)
			}
			wantJSON := `"verdict": "` + tt.wantVerdict + `"`
			if !strings.Contains(string(jsonBytes), wantJSON) {
				t.Errorf("RenderJSON output missing %q\ngot:\n%s", wantJSON, string(jsonBytes))
			}

			// Exit-code policy (verdict=="ok" uses handleLGTM, not applyVerdictExitPolicy;
			// test that non-blocking advisory downgrades to 0, blocking stays 1).
			if tt.wantVerdict != "ok" {
				findingsCode := domain.ExitFindings
				got := applyVerdictExitPolicy(g.Verdict, false, findingsCode)
				if got != tt.wantExitPolicy {
					t.Errorf("applyVerdictExitPolicy(%q, false, ExitFindings) = %d, want %d",
						g.Verdict, got, tt.wantExitPolicy)
				}
			}
		})
	}
}

// --- New auto-phase knob tests (large_diff_groups / medium_diff_reviewers) ---

// TestResolveAutoPhase_LargeUsesLargeDiffGroupsKnob verifies that the large_diff_groups
// knob caps the grouped diff path. With many files / lines the greedy packer
// would normally produce > largeDiffGroups groups; largeDiffGroups=3 caps the result.
func TestResolveAutoPhase_LargeUsesLargeDiffGroupsKnob(t *testing.T) {
	// 25 files × 50 lines (1250 lines, default 200/group) → ~7 groups
	// without cap; large_diff_groups=3 caps to 3.
	sections := makeSectionsForReview(25, 50)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr, _ := resolveAutoPhase(git.DiffSizeLarge, fullDiff, "", true, testBuildAgents(agents[0], agents), 3, 2)

	if !apr.UseGrouped {
		t.Fatalf("expected UseGrouped=true; FallbackReason=%q", apr.FallbackReason)
	}
	diffGroupCount := len(apr.GroupedSpecs) - 1
	if diffGroupCount > 3 {
		t.Errorf("expected at most 3 diff groups (large_diff_groups=3 cap), got %d", diffGroupCount)
	}
	if diffGroupCount < 2 {
		t.Errorf("expected >=2 diff groups, got %d", diffGroupCount)
	}
}

// TestResolveAutoPhase_LargeFallbackToMedium_WhenFileCountTooSmall verifies
// that when file_count<2 the grouped path is impossible and falls back to
// medium carrying medium_diff_reviewers as MediumDiffCount.
func TestResolveAutoPhase_LargeFallbackToMedium_WhenFileCountTooSmall(t *testing.T) {
	// 1 file, large_diff_groups=4 → effectiveGroups=1 → fallback.
	sections := makeSectionsForReview(1, 10)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr, _ := resolveAutoPhase(git.DiffSizeLarge, fullDiff, "", true, testBuildAgents(agents[0], agents), 4, 3)

	if apr.UseGrouped {
		t.Fatal("expected UseGrouped=false when fileCount<2")
	}
	if apr.PhaseStr != "medium" {
		t.Errorf("expected PhaseStr 'medium', got %q", apr.PhaseStr)
	}
	if apr.MediumDiffCount != 3 {
		t.Errorf("expected MediumDiffCount=3 (from medium_diff_reviewers), got %d", apr.MediumDiffCount)
	}
	if apr.FallbackReason == "" {
		t.Error("expected non-empty FallbackReason")
	}
}

// TestResolveAutoPhase_MediumUsesMediumDiffReviewersKnob verifies that medium
// path returns medium with MediumDiffCount=mediumDiffReviewers.
func TestResolveAutoPhase_MediumUsesMediumDiffReviewersKnob(t *testing.T) {
	apr, _ := resolveAutoPhase(git.DiffSizeMedium, "", "", true, testBuildAgents(nil, nil), 4, 4)

	if apr.UseGrouped {
		t.Fatal("expected UseGrouped=false on medium")
	}
	if apr.PhaseStr != "medium" {
		t.Errorf("expected PhaseStr 'medium', got %q", apr.PhaseStr)
	}
	if apr.MediumDiffCount != 4 {
		t.Errorf("expected MediumDiffCount=4, got %d", apr.MediumDiffCount)
	}
}

// TestResolveAutoPhase_LargeRespectsFileCountUpperBound verifies that
// effectiveGroups = min(largeDiffGroups, fileCount). With large_diff_groups>>fileCount
// the cap is fileCount; the resulting group count never exceeds fileCount.
func TestResolveAutoPhase_LargeRespectsFileCountUpperBound(t *testing.T) {
	// 12 files (> default 5 files/group) so the greedy packer naturally
	// produces multiple groups; large_diff_groups=10 is capped to fileCount=12.
	// We assert the resulting count never exceeds fileCount and remains
	// within the large_diff_groups cap.
	sections := makeSectionsForReview(12, 10)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr, _ := resolveAutoPhase(git.DiffSizeLarge, fullDiff, "", true, testBuildAgents(agents[0], agents), 10, 2)

	if !apr.UseGrouped {
		t.Fatalf("expected UseGrouped=true with 12 files / large_diff_groups=10; FallbackReason=%q", apr.FallbackReason)
	}
	diffGroupCount := len(apr.GroupedSpecs) - 1
	if diffGroupCount > 12 {
		t.Errorf("expected diff group count capped at fileCount=12, got %d", diffGroupCount)
	}
	if diffGroupCount > 10 {
		t.Errorf("expected diff group count capped at large_diff_groups=10, got %d", diffGroupCount)
	}
	if diffGroupCount < 2 {
		t.Errorf("expected >=2 diff groups, got %d", diffGroupCount)
	}
}

// TestResolveAutoPhase_Small returns flat diff regardless of knobs.
func TestResolveAutoPhase_Small(t *testing.T) {
	apr, _ := resolveAutoPhase(git.DiffSizeSmall, "irrelevant", "", true, testBuildAgents(nil, nil), 4, 2)
	if apr.UseGrouped {
		t.Errorf("expected UseGrouped=false for small")
	}
	if apr.PhaseStr != "small" {
		t.Errorf("expected PhaseStr 'small', got %q", apr.PhaseStr)
	}
	if apr.MediumDiffCount != 0 {
		t.Errorf("expected MediumDiffCount=0 on small, got %d", apr.MediumDiffCount)
	}
}

// TestBuildGroupedDiffSpecs_EmptyGroupsSkipped_IDsContiguous verifies that when
// buildGroupedDiffSpecs skips empty groups, the surviving specs have contiguous
// ReviewerIDs (1,2,3,...). Round-9 contract: specs[0] is the arch spec
// (always archAgent); specs[1..] round-robin through diffAgents starting from
// diffAgents[0] for the 1st surviving diff group.
func TestBuildGroupedDiffSpecs_EmptyGroupsSkipped_IDsContiguous(t *testing.T) {
	sections := makeSectionsForReview(6, 10)
	fullDiff := git.JoinDiffSections(sections)
	diffAgents := []agent.Agent{agent.NewCodexAgent(""), agent.NewCodexAgent("")}
	archAgent := agent.NewCodexAgent("")

	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, archAgent, diffAgents, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// IDs must be 1..N contiguous.
	for i, s := range specs {
		wantID := i + 1
		if s.ReviewerID != wantID {
			t.Errorf("specs[%d].ReviewerID = %d, want %d", i, s.ReviewerID, wantID)
		}
	}
	// Arch spec uses archAgent.
	if specs[0].Agent != archAgent {
		t.Errorf("specs[0].Agent mismatch: expected archAgent")
	}
	// Diff specs round-robin diffAgents starting at index 0.
	for i := 1; i < len(specs); i++ {
		want := agent.AgentForReviewer(diffAgents, i)
		if specs[i].Agent != want {
			t.Errorf("specs[%d].Agent mismatch: expected diffAgents[%d]", i, (i-1)%len(diffAgents))
		}
	}
}

// TestNoAutoPhase_ProducesFlatPath_WithVerdict verifies that when AutoPhase=false
// and Phase="" the shouldUseAutoPhase gate returns false (flat path), and that
// ComputeVerdict still produces a non-empty verdict on the resulting GroupedFindings.
func TestNoAutoPhase_ProducesFlatPath_WithVerdict(t *testing.T) {
	// Flat path config: AutoPhase=false, no explicit Phase.
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{AutoPhase: false},
		Phase:          "",
	}
	if shouldUseAutoPhase(opts) {
		t.Fatal("expected shouldUseAutoPhase=false for AutoPhase=false + empty Phase")
	}

	// Simulate findings that would arrive from the flat runner path.
	g := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "unused import", Severity: "advisory", Sources: []int{0}},
		},
	}
	g.ComputeVerdict(false, false)

	if g.Verdict == "" {
		t.Error("expected non-empty verdict after ComputeVerdict on flat-path findings")
	}
	// advisory without strict → exit 0
	got := applyVerdictExitPolicy(g.Verdict, false, domain.ExitFindings)
	if got != domain.ExitNoFindings {
		t.Errorf("flat-path advisory verdict should exit 0 (non-strict), got %d", got)
	}
}

// computeVerdictWithCCSignals mirrors the boundary logic in executeReview that
// folds IsDegraded into ccAdvisory before calling ComputeVerdict. Extracted
// only for testing; the production path lives at cmd/acr/review.go:~420.
func computeVerdictWithCCSignals(g *domain.GroupedFindings, cc *summarizer.CrossCheckResult) {
	ccBlocking := cc.HasBlockingFindings()
	ccAdvisory := cc != nil && !ccBlocking && (cc.HasAdvisoryFindings() || cc.IsDegraded())
	g.ComputeVerdict(ccBlocking, ccAdvisory)
}

func TestComputeVerdict_CrossCheckPartial_ForcesAdvisory(t *testing.T) {
	g := &domain.GroupedFindings{}
	cc := &summarizer.CrossCheckResult{
		Partial:      true,
		FailedAgents: []string{"codex"},
		// No findings at all, but some agents failed → degraded.
	}
	computeVerdictWithCCSignals(g, cc)
	if g.Verdict != "advisory" {
		t.Errorf("degraded (partial) cross-check with clean grouped must force advisory, got %q", g.Verdict)
	}
	if g.Ok == false {
		t.Errorf("advisory verdict should keep Ok=true, got false")
	}
}

func TestComputeVerdict_CrossCheckAllAgentsFailed_ForcesAdvisory(t *testing.T) {
	g := &domain.GroupedFindings{}
	cc := &summarizer.CrossCheckResult{
		Skipped:    true,
		SkipReason: "all 3 agents failed: codex: x; claude: y; gemini: z",
	}
	computeVerdictWithCCSignals(g, cc)
	if g.Verdict != "advisory" {
		t.Errorf("all-agents-failed cross-check must force advisory, got %q", g.Verdict)
	}
}

func TestComputeVerdict_CrossCheckStructuralSkip_KeepsOk(t *testing.T) {
	g := &domain.GroupedFindings{}
	cc := &summarizer.CrossCheckResult{
		Skipped:    true,
		SkipReason: summarizer.SkipReasonSingleGroup,
	}
	computeVerdictWithCCSignals(g, cc)
	if g.Verdict != "ok" {
		t.Errorf("structural cross-check skip must keep verdict=ok, got %q", g.Verdict)
	}
}

func TestComputeVerdict_CrossCheckBlockingBeatsDegraded(t *testing.T) {
	g := &domain.GroupedFindings{}
	cc := &summarizer.CrossCheckResult{
		Partial:      true,
		FailedAgents: []string{"codex"},
		Findings: []summarizer.CrossCheckFinding{
			{Title: "blocker", Severity: "blocking"},
		},
	}
	computeVerdictWithCCSignals(g, cc)
	if g.Verdict != "blocking" {
		t.Errorf("blocking finding must take precedence over degraded, got %q", g.Verdict)
	}
}

// --- Round-9: per-phase reviewer agent override tests ---

// TestBuildGroupedDiffSpecs_UsesArchAndDiffAgents verifies that when a
// distinct archAgent and diffAgents slice are supplied, the arch spec uses
// archAgent and the diff specs round-robin through diffAgents independently
// of archAgent.
func TestBuildGroupedDiffSpecs_UsesArchAndDiffAgents(t *testing.T) {
	sections := makeSectionsForReview(8, 10)
	fullDiff := git.JoinDiffSections(sections)

	archAgent := agent.NewClaudeAgent("")
	diffAgent1 := agent.NewCodexAgent("")
	diffAgent2 := agent.NewGeminiAgent("")
	diffAgents := []agent.Agent{diffAgent1, diffAgent2}

	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, archAgent, diffAgents, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) < 3 {
		t.Fatalf("expected >=1 arch + 2 diff specs, got %d", len(specs))
	}
	if specs[0].Agent != archAgent {
		t.Errorf("specs[0].Agent should be archAgent (claude), got %T name=%q", specs[0].Agent, specs[0].Agent.Name())
	}
	// Diff specs round-robin over diffAgents starting at index 0 (1-based reviewerIdx).
	for i := 1; i < len(specs); i++ {
		want := diffAgents[(i-1)%len(diffAgents)]
		if specs[i].Agent != want {
			t.Errorf("specs[%d].Agent = %s, want %s", i, specs[i].Agent.Name(), want.Name())
		}
	}
}

// TestBuildGroupedDiffSpecs_DiffAgentRoundRobinIndependent verifies that
// the diff-phase round-robin starts from diffAgents[0] regardless of
// archAgent identity (i.e. arch occupying ReviewerID=1 must not consume a
// diff-agent slot).
func TestBuildGroupedDiffSpecs_DiffAgentRoundRobinIndependent(t *testing.T) {
	sections := makeSectionsForReview(6, 10)
	fullDiff := git.JoinDiffSections(sections)

	archAgent := agent.NewClaudeAgent("")
	diffAgent1 := agent.NewCodexAgent("")
	diffAgent2 := agent.NewGeminiAgent("")
	diffAgents := []agent.Agent{diffAgent1, diffAgent2}

	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, archAgent, diffAgents, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) < 3 {
		t.Fatalf("expected >=3 specs, got %d", len(specs))
	}
	// First diff spec (index 1) MUST be diffAgents[0] = codex,
	// not gemini (which would happen if reviewerID=2 was used directly with
	// the round-robin).
	if specs[1].Agent != diffAgent1 {
		t.Errorf("first diff spec should use diffAgents[0]=codex, got %s", specs[1].Agent.Name())
	}
	if specs[2].Agent != diffAgent2 {
		t.Errorf("second diff spec should use diffAgents[1]=gemini, got %s", specs[2].Agent.Name())
	}
}

func TestMaxPotentialReviewers(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.ResolvedConfig
		want int
	}{
		{
			name: "flat dominates",
			cfg:  config.ResolvedConfig{Reviewers: 5, LargeDiffGroups: 3, MediumDiffReviewers: 2, SmallDiffReviewers: 1},
			want: 5, // max(5, 1+3, 1+2, 1) = 5
		},
		{
			name: "grouped dominates",
			cfg:  config.ResolvedConfig{Reviewers: 3, LargeDiffGroups: 8, MediumDiffReviewers: 2, SmallDiffReviewers: 1},
			want: 9, // max(3, 1+8, 1+2, 1) = 9
		},
		{
			name: "medium dominates",
			cfg:  config.ResolvedConfig{Reviewers: 2, LargeDiffGroups: 1, MediumDiffReviewers: 5, SmallDiffReviewers: 1},
			want: 6, // max(2, 1+1, 1+5, 1) = 6
		},
		{
			name: "all equal",
			cfg:  config.ResolvedConfig{Reviewers: 3, LargeDiffGroups: 2, MediumDiffReviewers: 2, SmallDiffReviewers: 1},
			want: 3, // max(3, 1+2, 1+2, 1) = 3
		},
		{
			name: "small dominates",
			cfg:  config.ResolvedConfig{Reviewers: 2, LargeDiffGroups: 1, MediumDiffReviewers: 1, SmallDiffReviewers: 10},
			want: 10, // max(2, 1+1, 1+1, 10) = 10
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxPotentialReviewers(tt.cfg)
			if got != tt.want {
				t.Errorf("maxPotentialReviewers() = %d, want %d", got, tt.want)
			}
		})
	}
}

// --- logPerPhaseModelMatrix tests ---
//
// Round-13 F#4: logPerPhaseModelMatrix evaluated opts.ReviewerAgents[0] BEFORE
// consulting ArchReviewerAgent, so a config with reviewer_agents=[] and
// arch_reviewer_agent="codex" caused a panic in verbose grouped-path logging.
// The fix re-orders the evaluation to mirror buildPhaseAgents (explicit arch
// override first, reviewer fallback second) and adds a guard for the fully-empty
// case so logging degrades gracefully rather than panicking.

func TestLogPerPhaseModelMatrix_EmptyReviewerAgentsWithArchOverride(t *testing.T) {
	// Empty ReviewerAgents + explicit ArchReviewerAgent must NOT panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logPerPhaseModelMatrix panicked: %v", r)
		}
	}()
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			ReviewerAgents:     nil,
			ArchReviewerAgent:  "codex",
			DiffReviewerAgents: []string{"claude"},
		},
	}
	logPerPhaseModelMatrix(terminal.NewLogger(), opts, "large")
}

func TestLogPerPhaseModelMatrix_ArchOverrideTakesPrecedence(t *testing.T) {
	// When both are set, arch override wins — matches buildPhaseAgents.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logPerPhaseModelMatrix panicked: %v", r)
		}
	}()
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			ReviewerAgents:    []string{"gemini", "codex"},
			ArchReviewerAgent: "claude",
		},
	}
	logPerPhaseModelMatrix(terminal.NewLogger(), opts, "large")
}

func TestLogPerPhaseModelMatrix_ReviewerAgentsFallback(t *testing.T) {
	// Without an explicit arch override, the first reviewer agent drives arch.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logPerPhaseModelMatrix panicked: %v", r)
		}
	}()
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			ReviewerAgents: []string{"codex", "claude"},
		},
	}
	logPerPhaseModelMatrix(terminal.NewLogger(), opts, "large")
}

func TestLogPerPhaseModelMatrix_BothEmpty_GracefulDegrade(t *testing.T) {
	// Pathological empty config: must not panic. A real run would already have
	// aborted in buildPhaseAgents, but logging runs defensively.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logPerPhaseModelMatrix panicked: %v", r)
		}
	}()
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			ReviewerAgents:     nil,
			ArchReviewerAgent:  "",
			DiffReviewerAgents: nil,
		},
	}
	logPerPhaseModelMatrix(terminal.NewLogger(), opts, "large")
}

// --- logCrossCheckModelMatrix tests ---
//
// Round-14 F#1 (案 A): logCrossCheckModelMatrix mirrors logPerPhaseModelMatrix
// for the cross_check verbose row. Like its sibling, it runs only after the
// grouped path is confirmed (sizeStr="large"), but defensive coding still
// requires panic-safety on every reachable input path so a logging failure
// never crashes a review. These tests pin the same invariants the Round-13
// F#4 tests pinned for logPerPhaseModelMatrix.

func TestLogCrossCheckModelMatrix_ResolveErrorEmitsWarningNoPanic(t *testing.T) {
	// Empty top-level CrossCheckModel + empty models tree → resolve fails for
	// every selected agent. Helper must emit a warning row and return cleanly.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logCrossCheckModelMatrix panicked: %v", r)
		}
	}()
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex",
			CrossCheckModel:   "", // forces tree cascade
			SummarizerAgent:   "codex",
			Models:            config.ModelsConfig{}, // empty: no path resolves
		},
	}
	logCrossCheckModelMatrix(terminal.NewLogger(), opts, "large")
}

func TestLogCrossCheckModelMatrix_AgentDefaultsToSummarizerWhenCrossCheckAgentEmpty(t *testing.T) {
	// CrossCheckAgent unset must fall back to SummarizerAgent inside the
	// resolver call from the log helper, mirroring runtime behavior.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logCrossCheckModelMatrix panicked: %v", r)
		}
	}()
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "", // empty → use summarizer agent
			SummarizerAgent:   "codex",
			CrossCheckModel:   "gpt-5.4-mini",
		},
	}
	logCrossCheckModelMatrix(terminal.NewLogger(), opts, "large")
}

func TestLogCrossCheckModelMatrix_EmitsOneRowPerResolvedAgent(t *testing.T) {
	// Normal grouped-path scenario: all selected agents resolve from the
	// agents tree. Helper must complete without panicking and produce one
	// entry per agent (smoke-tested by simply observing no panic).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logCrossCheckModelMatrix panicked: %v", r)
		}
	}()
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			CrossCheckEnabled: true,
			CrossCheckAgent:   "codex,claude",
			CrossCheckModel:   "",
			SummarizerAgent:   "codex",
			Models: config.ModelsConfig{
				Agents: map[string]config.RoleModels{
					"codex":  {CrossCheck: &config.ModelSpec{Model: "gpt-5.4-mini"}},
					"claude": {CrossCheck: &config.ModelSpec{Model: "claude-opus-4-7"}},
				},
			},
		},
	}
	logCrossCheckModelMatrix(terminal.NewLogger(), opts, "large")
}
