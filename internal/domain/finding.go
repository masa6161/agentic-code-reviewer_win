package domain

import (
	"slices"
	"strings"
)

// Finding represents a single review finding from a reviewer iteration.
type Finding struct {
	Text       string
	ReviewerID int
	Severity   string // "blocking" | "advisory" | "" (empty implies advisory)
	Prefix     string // "[must]" | "[imo]" | "[nits]" | "[fyi]" | "[ask]" | ""
	Category   string // "correctness" | "security" | "perf" | "maintainability" | "testing" | "style" | ""
	Phase      string // "arch" | "diff" | "" (populated from ReviewConfig.Phase)
	GroupKey   string `json:"group_key,omitempty"` // "arch" | "g01"..."gNN" | "" (populated from ReviewerSpec.GroupKey)
}

// AggregatedFinding represents a finding with the list of reviewers who found it.
type AggregatedFinding struct {
	Text          string
	Reviewers     []int
	ArchReviewers []int  `json:"arch_reviewers,omitempty"`
	DiffReviewers []int  `json:"diff_reviewers,omitempty"`
	Severity      string // first-seen severity from grouped findings
	GroupKey      string // group key(s), comma-separated if from multiple groups
}

// FindingGroup represents a grouped/clustered finding from the summarizer.
type FindingGroup struct {
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	Messages          []string `json:"messages"`
	ReviewerCount     int      `json:"reviewer_count"`
	ArchReviewerCount int      `json:"arch_reviewer_count,omitempty"`
	DiffReviewerCount int      `json:"diff_reviewer_count,omitempty"`
	Sources           []int    `json:"sources"`
	GroupKey          string   `json:"group_key,omitempty"` // propagated from source findings
	Severity          string   `json:"severity,omitempty"`  // "blocking" | "advisory" | "" (empty = advisory)
}

// GroupedFindings represents the output from the summarizer.
type GroupedFindings struct {
	Findings           []FindingGroup `json:"findings"`
	Info               []FindingGroup `json:"info"`
	Ok                 bool           `json:"ok"`
	Verdict            string         `json:"verdict"` // "blocking" | "advisory" | "ok"
	NotesForNextReview string         `json:"notes_for_next_review"`
	SkippedFiles       []string       `json:"skipped_files"`
}

// ComputeVerdict derives Verdict from Findings + an optional cross-check signal.
// - blocking if any Finding.Severity=="blocking" OR ccBlocking=true
// - advisory if any finding exists (all advisory) OR ccAdvisory=true (but not blocking)
// - ok otherwise
// Also sets Ok = (Verdict != "blocking") for backward compatibility.
func (g *GroupedFindings) ComputeVerdict(ccBlocking, ccAdvisory bool) {
	hasBlocking := ccBlocking
	hasAny := ccAdvisory || ccBlocking
	for _, f := range g.Findings {
		hasAny = true
		if f.Severity == "blocking" {
			hasBlocking = true
		}
	}
	switch {
	case hasBlocking:
		g.Verdict = "blocking"
	case hasAny:
		g.Verdict = "advisory"
	default:
		g.Verdict = "ok"
	}
	g.Ok = g.Verdict != "blocking"
	if g.SkippedFiles == nil {
		g.SkippedFiles = []string{}
	}
}

// HasFindings returns true if there are any findings.
func (g *GroupedFindings) HasFindings() bool {
	return len(g.Findings) > 0
}

// HasInfo returns true if there are any informational notes.
func (g *GroupedFindings) HasInfo() bool {
	return len(g.Info) > 0
}

// TotalGroups returns the total count of finding groups and info groups.
func (g *GroupedFindings) TotalGroups() int {
	return len(g.Findings) + len(g.Info)
}

// DispositionKind describes what happened to an aggregated finding in the pipeline.
type DispositionKind int

const (
	DispositionUnmapped        DispositionKind = iota // Could not trace through pipeline (zero value)
	DispositionInfo                                   // Categorized as informational by summarizer
	DispositionFilteredFP                             // Removed by FP filter
	DispositionFilteredExclude                        // Removed by exclude pattern
	DispositionSurvived                               // Survived all filters (became a posted finding)
)

// Disposition describes the pipeline outcome of an aggregated finding.
type Disposition struct {
	Kind       DispositionKind
	FPScore    int    // Only set for DispositionFilteredFP
	Reasoning  string // Only set for DispositionFilteredFP
	GroupTitle string
}

// FPRemovedInfo captures metadata about a finding group removed by the FP filter.
type FPRemovedInfo struct {
	Sources   []int
	FPScore   int
	Reasoning string
	Title     string
}

// BuildDispositions maps each aggregated finding index to its pipeline disposition.
func BuildDispositions(
	aggregatedCount int,
	infoGroups []FindingGroup,
	fpRemoved []FPRemovedInfo,
	excludeFiltered []FindingGroup,
	survivingFindings []FindingGroup,
) map[int]Disposition {
	dispositions := make(map[int]Disposition, aggregatedCount)

	// 1. Mark info groups
	for _, g := range infoGroups {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionInfo,
				GroupTitle: g.Title,
			}
		}
	}

	// 2. Mark FP-filtered
	for _, fp := range fpRemoved {
		for _, src := range fp.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionFilteredFP,
				FPScore:    fp.FPScore,
				Reasoning:  fp.Reasoning,
				GroupTitle: fp.Title,
			}
		}
	}

	// 3. Mark exclude-filtered
	for _, g := range excludeFiltered {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionFilteredExclude,
				GroupTitle: g.Title,
			}
		}
	}

	// 4. Mark survivors
	for _, g := range survivingFindings {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionSurvived,
				GroupTitle: g.Title,
			}
		}
	}

	// 5. Fill remaining unmapped indices (zero value is DispositionUnmapped)
	for i := range aggregatedCount {
		if _, ok := dispositions[i]; !ok {
			dispositions[i] = Disposition{}
		}
	}

	return dispositions
}

// BackfillPhaseReviewerCounts populates ArchReviewerCount and DiffReviewerCount
// on each FindingGroup by partitioning source reviewer IDs by phase.
func BackfillPhaseReviewerCounts(
	grouped *GroupedFindings,
	aggregated []AggregatedFinding,
	reviewerPhases map[int]string,
) {
	fill := func(groups []FindingGroup) {
		for i := range groups {
			archSet := make(map[int]struct{})
			diffSet := make(map[int]struct{})
			for _, srcIdx := range groups[i].Sources {
				if srcIdx < 0 || srcIdx >= len(aggregated) {
					continue
				}
				for _, rid := range aggregated[srcIdx].Reviewers {
					switch reviewerPhases[rid] {
					case PhaseArch:
						archSet[rid] = struct{}{}
					case PhaseDiff:
						diffSet[rid] = struct{}{}
					}
				}
			}
			groups[i].ArchReviewerCount = len(archSet)
			groups[i].DiffReviewerCount = len(diffSet)
		}
	}
	fill(grouped.Findings)
	fill(grouped.Info)
}

// addGroupKeyTokens splits raw by "," and inserts each non-empty trimmed token into tokens.
func addGroupKeyTokens(tokens map[string]struct{}, raw string) {
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tokens[t] = struct{}{}
		}
	}
}

// AggregateFindings aggregates findings by text, tracking which reviewers found each.
func AggregateFindings(findings []Finding) []AggregatedFinding {
	seen := make(map[string][]int)
	severities := make(map[string]string)
	groupKeyTokens := make(map[string]map[string]struct{})
	archRevs := make(map[string][]int)
	diffRevs := make(map[string][]int)
	order := make([]string, 0)

	for _, f := range findings {
		normalized := f.Text
		if normalized == "" {
			continue
		}

		reviewers, exists := seen[normalized]
		if !exists {
			order = append(order, normalized)
			reviewers = nil
			severities[normalized] = f.Severity
			groupKeyTokens[normalized] = map[string]struct{}{}
			addGroupKeyTokens(groupKeyTokens[normalized], f.GroupKey)
		} else {
			if f.Severity == "blocking" {
				severities[normalized] = "blocking"
			}
			addGroupKeyTokens(groupKeyTokens[normalized], f.GroupKey)
		}

		found := false
		for _, r := range reviewers {
			if r == f.ReviewerID {
				found = true
				break
			}
		}
		if !found {
			seen[normalized] = append(reviewers, f.ReviewerID)
			switch f.Phase {
			case PhaseArch:
				archRevs[normalized] = append(archRevs[normalized], f.ReviewerID)
			case PhaseDiff:
				diffRevs[normalized] = append(diffRevs[normalized], f.ReviewerID)
			default:
				diffRevs[normalized] = append(diffRevs[normalized], f.ReviewerID)
			}
		}
	}

	result := make([]AggregatedFinding, 0, len(order))
	for _, text := range order {
		reviewers := seen[text]
		sortedReviewers := slices.Clone(reviewers)
		slices.Sort(sortedReviewers)
		tokens := make([]string, 0, len(groupKeyTokens[text]))
		for tok := range groupKeyTokens[text] {
			tokens = append(tokens, tok)
		}
		slices.Sort(tokens)
		sortedArch := slices.Clone(archRevs[text])
		slices.Sort(sortedArch)
		sortedDiff := slices.Clone(diffRevs[text])
		slices.Sort(sortedDiff)
		result = append(result, AggregatedFinding{
			Text:          text,
			Reviewers:     sortedReviewers,
			ArchReviewers: sortedArch,
			DiffReviewers: sortedDiff,
			Severity:      severities[text],
			GroupKey:      strings.Join(tokens, ","),
		})
	}

	return result
}
