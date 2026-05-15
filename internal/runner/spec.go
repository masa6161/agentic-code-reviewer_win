package runner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/masa6161/arc-cli/internal/agent"
)

// FormatDistributionFromSpecs returns a human-readable distribution summary
// derived from the actual reviewer specs. Unlike agent.FormatDistribution
// (which simulates round-robin), this counts the real agent assignments.
// Returns "" when specs is empty or only a single agent type is present.
func FormatDistributionFromSpecs(specs []ReviewerSpec) string {
	if len(specs) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, s := range specs {
		if s.Agent == nil {
			continue
		}
		counts[s.Agent.Name()]++
	}
	if len(counts) <= 1 {
		return ""
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d×%s", counts[name], name))
	}
	return strings.Join(parts, ", ")
}

// ReviewerSpec defines the configuration for a single reviewer instance.
// It replaces round-robin agent assignment with explicit per-reviewer specs.
type ReviewerSpec struct {
	ReviewerID      int         // Authoritative reviewer ID; 0 means "assign from index+1 at runner construction"
	Agent           agent.Agent // Which agent to use
	Model           string      // Per-reviewer model override (empty = agent default)
	Phase           string      // "arch" | "diff" | "" (default = diff)
	GroupKey        string      // "arch", "g01", "g02", ... "full" for non-grouped
	Guidance        string      // Phase-specific role instructions
	Diff            string      // Reviewer-specific diff content (empty = global diff)
	DiffPrecomputed bool        // Whether Diff was pre-computed for this spec
	TargetFiles     []string    // Grouped diff target files (empty = full diff)
}
