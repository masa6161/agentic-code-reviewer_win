package modelconfig

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func ms(model, effort string) *config.ModelSpec {
	return &config.ModelSpec{Model: model, Effort: effort}
}

func TestResolve(t *testing.T) {
	type args struct {
		cfgModels    config.ModelsConfig
		size         string
		role         string
		agentName    string
		cliModel     string
		cliEffort    string
		legacyModel  string
		legacyEffort string
	}
	tests := []struct {
		name string
		args args
		want Spec
	}{
		{
			name: "all layers empty returns zero Spec",
			args: args{
				cfgModels: config.ModelsConfig{},
				role:      RoleReviewer,
				agentName: "codex",
			},
			want: Spec{},
		},
		{
			name: "legacy model only",
			args: args{
				cfgModels:   config.ModelsConfig{},
				role:        RoleReviewer,
				agentName:   "codex",
				legacyModel: "old-model",
			},
			want: Spec{Model: "old-model"},
		},
		{
			name: "defaults override legacy",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer: ms("new", ""),
					},
				},
				role:        RoleReviewer,
				agentName:   "codex",
				legacyModel: "old",
			},
			want: Spec{Model: "new"},
		},
		{
			name: "sizes override defaults",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer: ms("x", ""),
					},
					Sizes: map[string]config.RoleModels{
						"large": {Reviewer: ms("large-only", "")},
					},
				},
				size:      "large",
				role:      RoleReviewer,
				agentName: "codex",
			},
			want: Spec{Model: "large-only"},
		},
		{
			name: "agents override sizes",
			args: args{
				cfgModels: config.ModelsConfig{
					Sizes: map[string]config.RoleModels{
						"large": {Reviewer: ms("large", "")},
					},
					Agents: map[string]config.RoleModels{
						"codex": {Reviewer: ms("codex-only", "")},
					},
				},
				size:      "large",
				role:      RoleReviewer,
				agentName: "codex",
			},
			want: Spec{Model: "codex-only"},
		},
		{
			name: "cli overrides agents",
			args: args{
				cfgModels: config.ModelsConfig{
					Agents: map[string]config.RoleModels{
						"codex": {Reviewer: ms("x", "")},
					},
				},
				role:      RoleReviewer,
				agentName: "codex",
				cliModel:  "flag",
			},
			want: Spec{Model: "flag"},
		},
		{
			name: "model and effort cascade independently",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer: ms("", "high"),
					},
					Agents: map[string]config.RoleModels{
						"codex": {Reviewer: ms("m", "")},
					},
				},
				role:      RoleReviewer,
				agentName: "codex",
			},
			want: Spec{Model: "m", Effort: "high"},
		},
		{
			name: "unknown role returns zero Spec",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer: ms("x", "y"),
					},
				},
				role:      "unknown_role",
				agentName: "codex",
			},
			want: Spec{},
		},
		{
			name: "unknown agent skips agents layer",
			args: args{
				cfgModels: config.ModelsConfig{
					Agents: map[string]config.RoleModels{
						"codex": {Reviewer: ms("c", "")},
					},
				},
				role:      RoleReviewer,
				agentName: "claude",
			},
			want: Spec{},
		},
		{
			name: "empty size skips sizes layer",
			args: args{
				cfgModels: config.ModelsConfig{
					Sizes: map[string]config.RoleModels{
						"large": {Reviewer: ms("x", "")},
					},
				},
				size:      "",
				role:      RoleReviewer,
				agentName: "codex",
			},
			want: Spec{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(
				tt.args.cfgModels, tt.args.size, tt.args.role, tt.args.agentName,
				tt.args.cliModel, tt.args.cliEffort,
				tt.args.legacyModel, tt.args.legacyEffort,
			)
			if got != tt.want {
				t.Errorf("Resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveReviewer(t *testing.T) {
	type args struct {
		cfgModels    config.ModelsConfig
		size         string
		phase        string
		agentName    string
		cliModel     string
		cliEffort    string
		legacyModel  string
		legacyEffort string
	}
	tests := []struct {
		name string
		args args
		want Spec
	}{
		{
			name: "flat review (phase=\"\") uses generic reviewer only, ignores arch_reviewer",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer:     ms("gen", "med"),
						ArchReviewer: ms("arch-m", "high"),
					},
				},
				phase:     "",
				agentName: "codex",
			},
			want: Spec{Model: "gen", Effort: "med"},
		},
		{
			name: "phase=arch prefers arch_reviewer over generic reviewer",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer:     ms("gen", "med"),
						ArchReviewer: ms("arch-m", "high"),
					},
				},
				phase:     domain.PhaseArch,
				agentName: "codex",
			},
			want: Spec{Model: "arch-m", Effort: "high"},
		},
		{
			name: "phase=arch falls back to generic reviewer when arch_reviewer unset",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer: ms("gen", "med"),
					},
				},
				phase:     domain.PhaseArch,
				agentName: "codex",
			},
			want: Spec{Model: "gen", Effort: "med"},
		},
		{
			name: "phase=diff prefers diff_reviewer over generic reviewer",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer:     ms("gen", "med"),
						DiffReviewer: ms("diff-m", "low"),
					},
				},
				phase:     domain.PhaseDiff,
				agentName: "codex",
			},
			want: Spec{Model: "diff-m", Effort: "low"},
		},
		{
			name: "arch_reviewer.Model only → effort filled from generic reviewer (same layer)",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer:     ms("gen", "med"),
						ArchReviewer: ms("arch-m", ""),
					},
				},
				phase:     domain.PhaseArch,
				agentName: "codex",
			},
			want: Spec{Model: "arch-m", Effort: "med"},
		},
		{
			name: "agents.codex.reviewer preempts defaults.arch_reviewer (agents layer: generic fallback wins when arch_reviewer not set there)",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						ArchReviewer: ms("arch-default", "high"),
					},
					Agents: map[string]config.RoleModels{
						"codex": {Reviewer: ms("codex-gen", "med")},
					},
				},
				phase:     domain.PhaseArch,
				agentName: "codex",
			},
			want: Spec{Model: "codex-gen", Effort: "med"},
		},
		{
			name: "agents.codex.arch_reviewer wins over agents.codex.reviewer when phase=arch",
			args: args{
				cfgModels: config.ModelsConfig{
					Agents: map[string]config.RoleModels{
						"codex": {
							Reviewer:     ms("codex-gen", "med"),
							ArchReviewer: ms("codex-arch", "high"),
						},
					},
				},
				phase:     domain.PhaseArch,
				agentName: "codex",
			},
			want: Spec{Model: "codex-arch", Effort: "high"},
		},
		{
			name: "sizes.large.diff_reviewer wins over defaults.reviewer for phase=diff",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer: ms("def-gen", "med"),
					},
					Sizes: map[string]config.RoleModels{
						"large": {DiffReviewer: ms("large-diff", "high")},
					},
				},
				size:      "large",
				phase:     domain.PhaseDiff,
				agentName: "codex",
			},
			want: Spec{Model: "large-diff", Effort: "high"},
		},
		{
			name: "legacy fields fall back for generic reviewer only (no phase match)",
			args: args{
				cfgModels:    config.ModelsConfig{},
				phase:        domain.PhaseArch,
				agentName:    "codex",
				legacyModel:  "legacy-m",
				legacyEffort: "legacy-e",
			},
			want: Spec{Model: "legacy-m", Effort: "legacy-e"},
		},
		{
			name: "cli overrides bypass phase logic entirely",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						ArchReviewer: ms("arch-m", "high"),
					},
				},
				phase:     domain.PhaseArch,
				agentName: "codex",
				cliModel:  "cli-m",
				cliEffort: "cli-e",
			},
			want: Spec{Model: "cli-m", Effort: "cli-e"},
		},
		{
			name: "per-layer fallback: agents.codex has arch_reviewer (model only), effort from generic reviewer at SAME layer",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer: ms("def-gen", "def-effort"),
					},
					Agents: map[string]config.RoleModels{
						"codex": {
							Reviewer:     ms("codex-gen", "codex-effort"),
							ArchReviewer: ms("codex-arch", ""),
						},
					},
				},
				phase:     domain.PhaseArch,
				agentName: "codex",
			},
			want: Spec{Model: "codex-arch", Effort: "codex-effort"},
		},
		{
			name: "unknown phase treated as flat (generic reviewer only)",
			args: args{
				cfgModels: config.ModelsConfig{
					Defaults: config.RoleModels{
						Reviewer:     ms("gen", "med"),
						ArchReviewer: ms("arch-m", "high"),
					},
				},
				phase:     "unknown",
				agentName: "codex",
			},
			want: Spec{Model: "gen", Effort: "med"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveReviewer(
				tt.args.cfgModels, tt.args.size, tt.args.phase, tt.args.agentName,
				tt.args.cliModel, tt.args.cliEffort,
				tt.args.legacyModel, tt.args.legacyEffort,
			)
			if got != tt.want {
				t.Errorf("ResolveReviewer() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
