package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
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
			name:      "single arch",
			phaseStr:  "arch",
			reviewers: 3,
			want:      []runner.PhaseConfig{{Phase: "arch", ReviewerCount: 1}},
		},
		{
			name:      "single diff",
			phaseStr:  "diff",
			reviewers: 3,
			want:      []runner.PhaseConfig{{Phase: "diff", ReviewerCount: 3}},
		},
		{
			name:      "arch,diff with enough reviewers",
			phaseStr:  "arch,diff",
			reviewers: 3,
			want: []runner.PhaseConfig{
				{Phase: "arch", ReviewerCount: 1},
				{Phase: "diff", ReviewerCount: 2},
			},
		},
		{
			name:      "arch,diff minimum reviewers",
			phaseStr:  "arch,diff",
			reviewers: 2,
			want: []runner.PhaseConfig{
				{Phase: "arch", ReviewerCount: 1},
				{Phase: "diff", ReviewerCount: 1},
			},
		},
		{
			name:      "unknown phase token",
			phaseStr:  "arch,dif",
			reviewers: 3,
			wantErr:   true,
		},
		{
			name:      "duplicate phase",
			phaseStr:  "arch,arch",
			reviewers: 3,
			wantErr:   true,
		},
		{
			name:      "budget exhausted by arch,diff with 1 reviewer",
			phaseStr:  "arch,diff",
			reviewers: 1,
			wantErr:   true,
		},
		{
			name:      "zero reviewers",
			phaseStr:  "diff",
			reviewers: 0,
			wantErr:   true,
		},
		{
			name:      "negative reviewers",
			phaseStr:  "arch",
			reviewers: -1,
			wantErr:   true,
		},
		{
			name:      "whitespace trimmed",
			phaseStr:  " arch , diff ",
			reviewers: 3,
			want: []runner.PhaseConfig{
				{Phase: "arch", ReviewerCount: 1},
				{Phase: "diff", ReviewerCount: 2},
			},
		},
		{
			name:      "empty parts ignored",
			phaseStr:  "arch,,diff",
			reviewers: 3,
			want: []runner.PhaseConfig{
				{Phase: "arch", ReviewerCount: 1},
				{Phase: "diff", ReviewerCount: 2},
			},
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
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents, 4)
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
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents, 4)
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
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents, 4)
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
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents, 2)
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
	specs, err := buildGroupedDiffSpecs(fullDiff, "", true, agents, 4)
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
	_, err := buildGroupedDiffSpecs("", "", true, agents, 4)
	if err == nil {
		t.Fatal("expected error for empty diff, got nil")
	}
}

// --- resolveAutoPhase tests ---

func TestAutoPhase_Large_GroupedSpecs(t *testing.T) {
	// Large diff with enough reviewers → grouped specs
	sections := makeSectionsForReview(12, 10)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr := resolveAutoPhase(git.DiffSizeLarge, 3, fullDiff, "", true, agents)

	if !apr.UseGrouped {
		t.Fatal("expected UseGrouped=true for large diff with 3 reviewers")
	}
	if apr.PhaseStr != "" {
		t.Errorf("expected empty PhaseStr, got %q", apr.PhaseStr)
	}
	if len(apr.GroupedSpecs) < 2 {
		t.Errorf("expected at least 2 specs (1 arch + 1+ diff), got %d", len(apr.GroupedSpecs))
	}
}

func TestAutoPhase_Large_FallbackTooFewReviewers(t *testing.T) {
	// Large diff with reviewers=1 → fallback to arch,diff
	apr := resolveAutoPhase(git.DiffSizeLarge, 1, "irrelevant", "", true, nil)

	if apr.UseGrouped {
		t.Fatal("expected UseGrouped=false for reviewers=1")
	}
	if apr.PhaseStr != "arch,diff" {
		t.Errorf("expected PhaseStr 'arch,diff', got %q", apr.PhaseStr)
	}
}

func TestAutoPhase_Large_FallbackOnError(t *testing.T) {
	// Large diff but empty diff content → buildGroupedDiffSpecs fails → fallback
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr := resolveAutoPhase(git.DiffSizeLarge, 3, "", "", true, agents)

	if apr.UseGrouped {
		t.Fatal("expected UseGrouped=false when buildGroupedDiffSpecs fails")
	}
	if apr.PhaseStr != "arch,diff" {
		t.Errorf("expected PhaseStr 'arch,diff', got %q", apr.PhaseStr)
	}
}

func TestAutoPhase_Large_MaxDiffGroupsClamped(t *testing.T) {
	// reviewers=6 → maxDiffGroups should be clamped to 4 (not 5)
	sections := makeSectionsForReview(20, 10)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}

	apr := resolveAutoPhase(git.DiffSizeLarge, 6, fullDiff, "", true, agents)

	if !apr.UseGrouped {
		t.Fatal("expected UseGrouped=true")
	}
	// 1 arch + at most 4 diff groups = max 5 specs
	diffGroupCount := len(apr.GroupedSpecs) - 1 // subtract arch
	if diffGroupCount > 4 {
		t.Errorf("expected at most 4 diff groups (clamped), got %d", diffGroupCount)
	}
}

// --- buildCrossCheckContext tests ---

func TestBuildCrossCheckContext_GroupTopology(t *testing.T) {
	specs := []runner.ReviewerSpec{
		{Phase: "arch", GroupKey: "arch"},
		{Phase: "diff", GroupKey: "g01", TargetFiles: []string{"a.go"}},
		{Phase: "diff", GroupKey: "g02", TargetFiles: []string{"b.go"}},
	}
	findings := []domain.Finding{
		{Text: "issue", ReviewerID: 2, GroupKey: "g01"},
	}
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Findings: []domain.Finding{}},
		{ReviewerID: 2, ExitCode: 0, Findings: findings},
		{ReviewerID: 3, ExitCode: 1, TimedOut: true},
	}

	ccCtx := buildCrossCheckContext(findings, specs, results)

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

// --- resolveCrossCheckAgents tests ---

func TestResolveCrossCheckAgents_EmptyFallsBackToSummarizer(t *testing.T) {
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			SummarizerAgent:  "codex",
			SummarizerModel:  "gpt-5",
			CrossCheckAgent:  "",
			CrossCheckModel:  "",
		},
	}
	names, model := resolveCrossCheckAgents(opts)
	if len(names) != 1 || names[0] != "codex" {
		t.Errorf("expected fallback to summarizer agent, got %v", names)
	}
	if model != "gpt-5" {
		t.Errorf("expected fallback to summarizer model, got %q", model)
	}
}

func TestResolveCrossCheckAgents_Explicit(t *testing.T) {
	opts := ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			SummarizerAgent: "codex",
			SummarizerModel: "gpt-5",
			CrossCheckAgent: "claude,gemini",
			CrossCheckModel: "opus",
		},
	}
	names, model := resolveCrossCheckAgents(opts)
	if len(names) != 2 || names[0] != "claude" || names[1] != "gemini" {
		t.Errorf("expected [claude gemini], got %v", names)
	}
	if model != "opus" {
		t.Errorf("expected 'opus', got %q", model)
	}
}

// Sanity test that grouped diff path produces specs whose GroupKey values
// match the buildCrossCheckContext expected topology (1 arch + N diff groups).
func TestLargeReview_GroupedSpecsProduceCrossCheckableTopology(t *testing.T) {
	sections := makeSectionsForReview(8, 5)
	fullDiff := git.JoinDiffSections(sections)
	agents := []agent.Agent{agent.NewCodexAgent("")}
	apr := resolveAutoPhase(git.DiffSizeLarge, 4, fullDiff, "", true, agents)
	if !apr.UseGrouped {
		t.Fatal("expected UseGrouped=true for large diff with 4 reviewers")
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
