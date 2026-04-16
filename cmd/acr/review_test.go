package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
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

// --- splitFindingsByPhase tests ---

func TestSplitFindingsByPhase(t *testing.T) {
	findings := []domain.Finding{
		{Text: "arch issue", Phase: "arch", ReviewerID: 1},
		{Text: "diff issue 1", Phase: "diff", ReviewerID: 2},
		{Text: "diff issue 2", Phase: "diff", ReviewerID: 3},
		{Text: "no phase", Phase: "", ReviewerID: 4},
	}

	arch, diff := splitFindingsByPhase(findings)

	if len(arch) != 1 {
		t.Errorf("expected 1 arch finding, got %d", len(arch))
	}
	if arch[0].Text != "arch issue" {
		t.Errorf("arch[0].Text = %q, want %q", arch[0].Text, "arch issue")
	}

	// diff should include both "diff" and "" phase findings
	if len(diff) != 3 {
		t.Errorf("expected 3 diff findings, got %d", len(diff))
	}
}

func TestSplitFindingsByPhase_AllArch(t *testing.T) {
	findings := []domain.Finding{
		{Text: "a1", Phase: "arch"},
		{Text: "a2", Phase: "arch"},
	}
	arch, diff := splitFindingsByPhase(findings)
	if len(arch) != 2 {
		t.Errorf("expected 2 arch, got %d", len(arch))
	}
	if len(diff) != 0 {
		t.Errorf("expected 0 diff, got %d", len(diff))
	}
}

func TestSplitFindingsByPhase_Empty(t *testing.T) {
	arch, diff := splitFindingsByPhase(nil)
	if arch != nil || diff != nil {
		t.Errorf("expected nil results for nil input")
	}
}

// --- mergeGroupedFindings tests ---

func TestMergeGroupedFindings_AllOk(t *testing.T) {
	a := &domain.GroupedFindings{
		Ok:       true,
		Findings: []domain.FindingGroup{{Title: "arch-f1"}},
		Info:     []domain.FindingGroup{{Title: "arch-i1"}},
	}
	b := &domain.GroupedFindings{
		Ok:       true,
		Findings: []domain.FindingGroup{{Title: "diff-f1"}},
	}

	merged := mergeGroupedFindings(a, b)
	if !merged.Ok {
		t.Error("expected merged.Ok = true")
	}
	if len(merged.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(merged.Findings))
	}
	if len(merged.Info) != 1 {
		t.Errorf("expected 1 info, got %d", len(merged.Info))
	}
}

func TestMergeGroupedFindings_PartialFail(t *testing.T) {
	a := &domain.GroupedFindings{Ok: true}
	b := &domain.GroupedFindings{Ok: false, Findings: []domain.FindingGroup{{Title: "bad"}}}

	merged := mergeGroupedFindings(a, b)
	if merged.Ok {
		t.Error("expected merged.Ok = false when one phase is not ok")
	}
}

func TestMergeGroupedFindings_NilGroup(t *testing.T) {
	a := &domain.GroupedFindings{
		Ok:       true,
		Findings: []domain.FindingGroup{{Title: "f1"}},
	}

	merged := mergeGroupedFindings(nil, a, nil)
	if !merged.Ok {
		t.Error("expected merged.Ok = true")
	}
	if len(merged.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(merged.Findings))
	}
}

func TestMergeGroupedFindings_NotesConcat(t *testing.T) {
	a := &domain.GroupedFindings{Ok: true, NotesForNextReview: "note-arch"}
	b := &domain.GroupedFindings{Ok: true, NotesForNextReview: "note-diff"}

	merged := mergeGroupedFindings(a, b)
	if merged.NotesForNextReview != "note-arch\nnote-diff" {
		t.Errorf("expected concatenated notes, got %q", merged.NotesForNextReview)
	}
}
