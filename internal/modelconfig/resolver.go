// Package modelconfig resolves model + effort specifications for a given
// (size, role, agentName) tuple by composing multiple configuration layers.
package modelconfig

import (
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// Role constants for the supported agent roles.
const (
	RoleReviewer     = "reviewer"
	RoleArchReviewer = "arch_reviewer"
	RoleDiffReviewer = "diff_reviewer"
	RoleSummarizer   = "summarizer"
	RoleFPFilter     = "fp_filter"
	RoleCrossCheck   = "cross_check"
	RolePRFeedback   = "pr_feedback"
)

// Spec is the resolved model + effort pair that agent constructors receive.
// Empty fields mean "agent default".
type Spec struct {
	Model  string
	Effort string
}

// Resolve composes a Spec for the given (size, role, agentName) tuple,
// honoring the following precedence (highest first):
//
//  1. cliModel / cliEffort: flag-level overrides (empty = not set)
//  2. cfgModels.Agents[agentName].<role>: agent-specific override
//  3. cfgModels.Sizes[size].<role>:       size-specific override
//  4. cfgModels.Defaults.<role>:          global default for the role
//  5. legacyModel / legacyEffort:         pre-round-5 flat fields
//     (reviewer_model, summarizer_model, etc)
//  6. zero Spec:                          agent's built-in default
//
// Model and Effort are resolved INDEPENDENTLY: if a higher-priority source
// sets only Model, Effort continues to cascade through lower priorities. This
// lets users set effort globally via defaults and override model per agent.
func Resolve(
	cfgModels config.ModelsConfig,
	size, role, agentName string,
	cliModel, cliEffort string,
	legacyModel, legacyEffort string,
) Spec {
	var model, effort string

	// Walk from highest to lowest priority; each layer fills in what's still empty.
	candidates := []Spec{
		{Model: cliModel, Effort: cliEffort},
		pickSpec(lookupRole(cfgModels.Agents, agentName), role),
		pickSpec(lookupRole(cfgModels.Sizes, size), role),
		pickSpec(&cfgModels.Defaults, role),
		{Model: legacyModel, Effort: legacyEffort},
	}

	for _, c := range candidates {
		if model == "" && c.Model != "" {
			model = c.Model
		}
		if effort == "" && c.Effort != "" {
			effort = c.Effort
		}
		if model != "" && effort != "" {
			break
		}
	}
	return Spec{Model: model, Effort: effort}
}

// ResolveReviewer is a phase-aware reviewer-role resolver.
//
// When phase is "arch" or "diff", the phase-specific role (arch_reviewer /
// diff_reviewer) is tried first at each cascade layer, and falls back to the
// generic reviewer role at the SAME cascade layer before descending to the
// next layer. When phase is "" (flat review), only the generic reviewer role
// is consulted.
//
// Legacy fields (legacyModel / legacyEffort) apply to the generic reviewer
// fallback only; phase-specific roles have no legacy fallback of their own.
//
// Model and Effort are resolved INDEPENDENTLY (same rule as Resolve): if a
// higher-priority layer supplies only Model, Effort keeps cascading through
// lower layers. Within a single layer, a phase-specific spec that sets only
// Model is filled in from the generic reviewer spec at the same layer for
// the missing field.
func ResolveReviewer(
	cfgModels config.ModelsConfig,
	size, phase, agentName string,
	cliModel, cliEffort string,
	legacyModel, legacyEffort string,
) Spec {
	var primaryRole string
	switch strings.ToLower(phase) {
	case domain.PhaseArch:
		primaryRole = RoleArchReviewer
	case domain.PhaseDiff:
		primaryRole = RoleDiffReviewer
	default:
		primaryRole = ""
	}

	// Per-layer helper: try primary role then fallback (generic reviewer),
	// filling each missing field from the generic reviewer at the same layer
	// before descending to the next layer.
	tryLayer := func(rm *config.RoleModels) Spec {
		if rm == nil {
			return Spec{}
		}
		if primaryRole != "" {
			s := pickSpec(rm, primaryRole)
			if s.Model != "" || s.Effort != "" {
				gen := pickSpec(rm, RoleReviewer)
				if s.Model == "" {
					s.Model = gen.Model
				}
				if s.Effort == "" {
					s.Effort = gen.Effort
				}
				return s
			}
		}
		return pickSpec(rm, RoleReviewer)
	}

	var model, effort string

	// Walk layers highest → lowest priority. Each layer fills in what's still
	// empty. CLI overrides bypass phase logic entirely.
	candidates := []Spec{
		{Model: cliModel, Effort: cliEffort},
		tryLayer(lookupRole(cfgModels.Agents, agentName)),
		tryLayer(lookupRole(cfgModels.Sizes, size)),
		tryLayer(&cfgModels.Defaults),
		// Legacy applies to generic reviewer fallback only.
		{Model: legacyModel, Effort: legacyEffort},
	}
	for _, c := range candidates {
		if model == "" && c.Model != "" {
			model = c.Model
		}
		if effort == "" && c.Effort != "" {
			effort = c.Effort
		}
		if model != "" && effort != "" {
			break
		}
	}
	return Spec{Model: model, Effort: effort}
}

// lookupRole safely returns a pointer to the RoleModels at the given key,
// or nil if the map or key is empty.
func lookupRole(m map[string]config.RoleModels, key string) *config.RoleModels {
	if len(m) == 0 || key == "" {
		return nil
	}
	rm, ok := m[key]
	if !ok {
		return nil
	}
	return &rm
}

// pickSpec extracts the ModelSpec for the given role from a RoleModels.
// Returns zero Spec{} if either input is nil or the role slot is nil.
func pickSpec(rm *config.RoleModels, role string) Spec {
	if rm == nil {
		return Spec{}
	}
	var src *config.ModelSpec
	switch strings.ToLower(role) {
	case RoleReviewer:
		src = rm.Reviewer
	case RoleArchReviewer:
		src = rm.ArchReviewer
	case RoleDiffReviewer:
		src = rm.DiffReviewer
	case RoleSummarizer:
		src = rm.Summarizer
	case RoleFPFilter:
		src = rm.FPFilter
	case RoleCrossCheck:
		src = rm.CrossCheck
	case RolePRFeedback:
		src = rm.PRFeedback
	}
	if src == nil {
		return Spec{}
	}
	return Spec{Model: src.Model, Effort: src.Effort}
}
