package runner

import "github.com/richhaase/agentic-code-reviewer/internal/agent"

// ReviewerSpec defines the configuration for a single reviewer instance.
// It replaces round-robin agent assignment with explicit per-reviewer specs.
type ReviewerSpec struct {
	Agent           agent.Agent // Which agent to use
	Model           string      // Per-reviewer model override (empty = agent default)
	Phase           string      // "arch" | "diff" | "" (default = diff)
	GroupKey        string      // "arch", "g01", "g02", ... "full" for non-grouped
	Guidance        string      // Phase-specific role instructions
	Diff            string      // Reviewer-specific diff content (empty = global diff)
	DiffPrecomputed bool        // Whether Diff was pre-computed for this spec
	TargetFiles     []string    // Grouped diff target files (empty = full diff)
}
