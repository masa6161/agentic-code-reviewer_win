package main

import (
	"testing"

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
