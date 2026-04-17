package summarizer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

// maxFindingTextLen is the maximum runes of a raw finding text shipped to the
// cross-check LLM. Long findings are truncated to keep stdin payloads small.
const maxFindingTextLen = 500

// Skip reason constants. Exported so callers (e.g., runner) can pattern-match
// structural skips instead of hardcoding string literals. Structural skips are
// conditions where cross-check is intentionally not run and must not suppress
// the LGTM banner; non-structural (error) skips must suppress it.
const (
	SkipReasonSingleGroup = "single group, cross-check unnecessary"
	SkipReasonNoAgents    = "no cross-check agents configured"
	// SkipReasonPayloadPrefix is a prefix; runtime reasons append ": " + err.
	SkipReasonPayloadPrefix = "payload marshal failed"
	// SkipReasonAllAgentsPrefix is a prefix for "all N agents failed: ..." strings.
	SkipReasonAllAgentsPrefix = "all "
)

// IsStructuralSkipReason reports whether a cross-check SkipReason represents a
// structural skip (intentional non-run) rather than a failure. Structural
// skips preserve LGTM; error skips suppress it.
func IsStructuralSkipReason(reason string) bool {
	return reason == "" || reason == SkipReasonSingleGroup
}

// truncateByRunes returns s truncated to at most max runes, appending "…" when
// truncation happens. max <= 0 returns s unchanged. Using rune-based truncation
// ensures multi-byte UTF-8 sequences (e.g. Japanese) are never split.
func truncateByRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// CrossCheckContext provides full context for cross-group verification.
// Findings holds post-aggregation data so that RelatedIDs in cross-check
// output refer to the same ID space used by the downstream summarizer.
type CrossCheckContext struct {
	Findings []domain.AggregatedFinding
	Groups   []GroupInfo
	Outcomes []GroupOutcome
}

// GroupInfo describes a review group's scope.
type GroupInfo struct {
	GroupKey    string
	Phase       string
	TargetFiles []string
	FullDiff    bool
}

// GroupOutcome describes a review group's execution result.
type GroupOutcome struct {
	GroupKey     string
	Succeeded    bool
	TimedOut     bool
	AuthFailed   bool
	FindingCount int
}

// CrossCheckResult contains the cross-group verification results.
// Follows fpfilter.Result pattern: Skipped+SkipReason for graceful degradation.
//
// Semantic convention:
//   - Skipped=true,  Partial=false: fully skipped (not run, single group, no agents, or all agents failed)
//   - Skipped=false, Partial=true:  some agents succeeded; Findings reflect only those agents
//   - Skipped=false, Partial=false: all agents succeeded
//
// SkipReason is populated when Skipped=true (full-skip context) or Partial=true (partial-failure context).
type CrossCheckResult struct {
	Findings     []CrossCheckFinding
	Skipped      bool
	Partial      bool     // true when some (but not all) agents failed; Findings reflect only successful agents
	FailedAgents []string // names of agents that errored out or timed out
	SkipReason   string
	AgentResults []AgentCrossCheckResult
	// Duration is wall-clock time (max across parallel agents).
	Duration time.Duration
}

// CrossCheckFinding represents a cross-group issue.
type CrossCheckFinding struct {
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Type           string   `json:"type"`
	InvolvedGroups []string `json:"involved_groups"`
	Severity       string   `json:"severity"`
	RelatedIDs     []int    `json:"related_finding_ids"`
}

// AgentCrossCheckResult records one agent's cross-check execution.
type AgentCrossCheckResult struct {
	AgentName string
	Findings  []CrossCheckFinding
	Duration  time.Duration
	Err       error
	Stderr    string
}

// HasBlockingFindings reports whether any cross-check finding is blocking.
func (r *CrossCheckResult) HasBlockingFindings() bool {
	if r == nil {
		return false
	}
	for _, f := range r.Findings {
		if f.Severity == "blocking" {
			return true
		}
	}
	return false
}

// HasAdvisoryFindings reports whether the cross-check produced any finding
// (blocking or advisory). Empty severity is treated as advisory.
func (r *CrossCheckResult) HasAdvisoryFindings() bool {
	if r == nil {
		return false
	}
	return len(r.Findings) > 0
}

// IsDegraded reports whether the cross-check ran but in a degraded state —
// either Partial (some agents failed) or Skipped for a non-structural reason
// (e.g. all agents failed, payload marshal failed). Degraded state should
// force at least advisory verdict since some cross-group concerns may be
// undetected.
func (r *CrossCheckResult) IsDegraded() bool {
	if r == nil {
		return false
	}
	if r.Partial {
		return true
	}
	if r.Skipped && !IsStructuralSkipReason(r.SkipReason) {
		return true
	}
	return false
}

const crossCheckPrompt = `# Cross-Check: Multi-Group Review Consistency Verification

You are verifying consistency across findings from a grouped code review.
A large change was split into groups, each reviewed independently.

Input JSON:
{
  "groups": [
    {"group_key": "arch", "phase": "arch", "full_diff": true, "succeeded": true, "finding_count": N},
    {"group_key": "g01", "phase": "diff", "target_files": ["a.go", "b.go"], "succeeded": true, "finding_count": N},
    ...
  ],
  "findings": [
    {"id": 0, "text": "finding text (truncated)", "phase": "arch", "group_key": "arch", "severity": "blocking"},
    ...
  ]
}

Key context:
- "arch" group reviewed the ENTIRE change for architectural concerns
- "diff" groups (g01, g02, ...) each reviewed a SUBSET of files
- Groups with succeeded=false or finding_count=0 may indicate blind spots

Tasks:
1. CONTRADICTIONS: findings from different groups giving mutually exclusive recommendations
2. GAPS: arch concerns not addressed by any succeeding diff group, OR cross-file issues split across group boundaries. A diff group with succeeded=false is a gap source (no data, not "no issues").
3. ESCALATIONS: findings whose severity should increase when seen in multi-group context (e.g., pattern affects files across multiple groups)

Output (JSON only, no prose):
{
  "findings": [
    {
      "title": "Short description",
      "summary": "Why this is a cross-group issue",
      "type": "contradiction|gap|escalation",
      "involved_groups": ["g01", "g02"],
      "severity": "blocking|advisory",
      "related_finding_ids": [0, 3]
    }
  ]
}

Rules:
- Return ONLY valid JSON
- Only report genuine cross-group issues - do not restate existing findings
- A diff group with succeeded=false means its coverage is unknown - flag as gap
- If no cross-group issues: {"findings": []}
`

// crossCheckGroupJSON, crossCheckFindingJSON, crossCheckPayload are the input
// payload shapes sent to each cross-check agent.
type crossCheckGroupJSON struct {
	GroupKey     string   `json:"group_key"`
	Phase        string   `json:"phase"`
	FullDiff     bool     `json:"full_diff,omitempty"`
	TargetFiles  []string `json:"target_files,omitempty"`
	Succeeded    bool     `json:"succeeded"`
	TimedOut     bool     `json:"timed_out,omitempty"`
	AuthFailed   bool     `json:"auth_failed,omitempty"`
	FindingCount int      `json:"finding_count"`
}

type crossCheckFindingJSON struct {
	ID       int    `json:"id"`
	Text     string `json:"text"`
	Phase    string `json:"phase,omitempty"`
	GroupKey string `json:"group_key,omitempty"`
	Severity string `json:"severity,omitempty"`
}

type crossCheckPayload struct {
	Groups   []crossCheckGroupJSON   `json:"groups"`
	Findings []crossCheckFindingJSON `json:"findings"`
}

type crossCheckResponse struct {
	Findings []CrossCheckFinding `json:"findings"`
}

// buildCrossCheckPayload converts a CrossCheckContext to the JSON payload
// shipped to each cross-check agent. Finding text is truncated to
// maxFindingTextLen characters.
func buildCrossCheckPayload(ccCtx CrossCheckContext) crossCheckPayload {
	outcomeByKey := make(map[string]GroupOutcome, len(ccCtx.Outcomes))
	for _, o := range ccCtx.Outcomes {
		outcomeByKey[o.GroupKey] = o
	}

	payload := crossCheckPayload{
		Groups:   make([]crossCheckGroupJSON, 0, len(ccCtx.Groups)),
		Findings: make([]crossCheckFindingJSON, 0, len(ccCtx.Findings)),
	}

	for _, g := range ccCtx.Groups {
		o := outcomeByKey[g.GroupKey]
		payload.Groups = append(payload.Groups, crossCheckGroupJSON{
			GroupKey:     g.GroupKey,
			Phase:        g.Phase,
			FullDiff:     g.FullDiff,
			TargetFiles:  g.TargetFiles,
			Succeeded:    o.Succeeded,
			TimedOut:     o.TimedOut,
			AuthFailed:   o.AuthFailed,
			FindingCount: o.FindingCount,
		})
	}

	for i, f := range ccCtx.Findings {
		text := truncateByRunes(f.Text, maxFindingTextLen)
		// AggregatedFinding has no Phase field; derive it from GroupKey.
		// "arch" group key → arch phase; anything else is a diff phase.
		phase := "diff"
		for _, tok := range strings.Split(f.GroupKey, ",") {
			if strings.TrimSpace(tok) == "arch" {
				phase = "arch"
				break
			}
		}
		payload.Findings = append(payload.Findings, crossCheckFindingJSON{
			ID:       i,
			Text:     text,
			Phase:    phase,
			GroupKey: f.GroupKey,
			Severity: f.Severity,
		})
	}

	return payload
}

// CrossCheck performs cross-group consistency verification.
// Multiple agents run in parallel; results are merged via union.
// Never returns error; uses Skipped/SkipReason for degradation.
func CrossCheck(
	ctx context.Context,
	agentNames []string,
	model string,
	ccCtx CrossCheckContext,
	verbose bool,
	logger *terminal.Logger,
) *CrossCheckResult {
	start := time.Now()

	if len(ccCtx.Groups) < 2 {
		return &CrossCheckResult{
			Skipped:    true,
			SkipReason: SkipReasonSingleGroup,
			Duration:   time.Since(start),
		}
	}
	if len(agentNames) == 0 {
		return &CrossCheckResult{
			Skipped:    true,
			SkipReason: SkipReasonNoAgents,
			Duration:   time.Since(start),
		}
	}

	payload := buildCrossCheckPayload(ccCtx)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return &CrossCheckResult{
			Skipped:    true,
			SkipReason: "payload marshal failed: " + err.Error(),
			Duration:   time.Since(start),
		}
	}

	var wg sync.WaitGroup
	agentResults := make([]AgentCrossCheckResult, len(agentNames))
	for i, name := range agentNames {
		wg.Add(1)
		go func(idx int, agentName string) {
			defer wg.Done()
			agentResults[idx] = runCrossCheckAgent(ctx, agentName, model, payloadBytes, verbose, logger)
		}(i, name)
	}
	wg.Wait()

	result := mergeAgentResults(agentResults, len(agentNames))
	result.AgentResults = agentResults
	result.Duration = time.Since(start)
	return result
}

// mergeAgentResults inspects a completed set of agent results and produces a
// CrossCheckResult with the correct Skipped/Partial/FailedAgents fields.
// It is extracted as a pure function to enable unit testing without real agents.
//
//   - 0 successes  → Skipped=true,  Partial=false
//   - some failures → Skipped=false, Partial=true,  FailedAgents populated
//   - all succeed  → Skipped=false, Partial=false, FailedAgents nil
func mergeAgentResults(agentResults []AgentCrossCheckResult, totalAgents int) *CrossCheckResult {
	var failedNames []string  // bare agent names, for FailedAgents field
	var failedDetail []string // "name: err" strings, for SkipReason
	var merged []CrossCheckFinding

	for _, ar := range agentResults {
		if ar.Err != nil {
			failedNames = append(failedNames, ar.AgentName)
			failedDetail = append(failedDetail, fmt.Sprintf("%s: %v", ar.AgentName, ar.Err))
			continue
		}
		merged = append(merged, ar.Findings...)
	}

	result := &CrossCheckResult{}

	if len(failedNames) == totalAgents {
		// All agents failed — fully skipped
		result.Skipped = true
		result.FailedAgents = failedNames
		result.SkipReason = fmt.Sprintf("all %d agents failed: %s",
			totalAgents, strings.Join(failedDetail, "; "))
		return result
	}

	if len(failedNames) > 0 {
		// Some agents failed — partial success
		result.Partial = true
		result.FailedAgents = failedNames
		result.SkipReason = fmt.Sprintf("%d of %d agents failed: %s",
			len(failedNames), totalAgents, strings.Join(failedDetail, "; "))
	}

	result.Findings = dedupCrossCheckFindings(merged)
	return result
}

// runCrossCheckAgent executes cross-check for a single agent and returns
// an AgentCrossCheckResult capturing its findings or error.
func runCrossCheckAgent(
	ctx context.Context,
	agentName, model string,
	payload []byte,
	verbose bool,
	logger *terminal.Logger,
) AgentCrossCheckResult {
	start := time.Now()
	result := AgentCrossCheckResult{AgentName: agentName}

	ag, err := agent.NewAgentWithModel(agentName, model)
	if err != nil {
		result.Err = fmt.Errorf("agent creation failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	if ctx.Err() != nil {
		result.Err = ctx.Err()
		result.Duration = time.Since(start)
		return result
	}

	execResult, err := ag.ExecuteSummary(ctx, crossCheckPrompt, payload)
	if err != nil {
		if ctx.Err() != nil {
			result.Err = ctx.Err()
		} else {
			result.Err = fmt.Errorf("execute failed: %w", err)
		}
		result.Duration = time.Since(start)
		return result
	}
	defer func() {
		if closeErr := execResult.Close(); closeErr != nil && verbose && logger != nil {
			logger.Logf(terminal.StyleDim, "cross-check close error (non-fatal): %v", closeErr)
		}
	}()

	output, err := io.ReadAll(execResult)
	if err != nil {
		if ctx.Err() != nil {
			result.Err = ctx.Err()
		} else {
			result.Err = fmt.Errorf("read failed: %w", err)
		}
		result.Duration = time.Since(start)
		return result
	}
	result.Stderr = execResult.Stderr()

	parser, err := agent.NewSummaryParser(agentName)
	if err != nil {
		result.Err = fmt.Errorf("parser creation failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	responseText, err := parser.ExtractText(output)
	if err != nil {
		result.Err = fmt.Errorf("extract failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	var response crossCheckResponse
	if err := json.Unmarshal([]byte(responseText), &response); err != nil {
		result.Err = fmt.Errorf("json unmarshal failed: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	result.Findings = response.Findings
	result.Duration = time.Since(start)
	return result
}

// normalizeTitle returns a normalized form of title suitable for use as a
// dedup key component. It lowercases, collapses whitespace, trims, and keeps
// at most 80 runes — enough to distinguish different issues while tolerating
// minor wording drift. If title is empty, it falls back to the first 80 runes
// of summary (normalized the same way). Returns "" when both are empty.
func normalizeTitle(title, summary string) string {
	s := title
	if strings.TrimSpace(s) == "" {
		s = summary
	}
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > 80 {
		runes = runes[:80]
	}
	return string(runes)
}

// dedupCrossCheckFindings merges findings that refer to the same issue.
// Two findings are considered the same when their Type, sorted RelatedIDs, and
// normalized title all match. Severity is upgraded (blocking beats advisory),
// InvolvedGroups and RelatedIDs are unioned, and Title/Summary content is
// preserved using a longer-wins strategy (distinct summaries are joined).
func dedupCrossCheckFindings(findings []CrossCheckFinding) []CrossCheckFinding {
	if len(findings) == 0 {
		return nil
	}

	type key struct {
		typeName    string
		idsKey      string
		titleNorm   string
	}

	keyFor := func(f CrossCheckFinding) key {
		ids := append([]int(nil), f.RelatedIDs...)
		sort.Ints(ids)
		parts := make([]string, len(ids))
		for i, v := range ids {
			parts[i] = fmt.Sprintf("%d", v)
		}
		return key{
			typeName:  f.Type,
			idsKey:    strings.Join(parts, ","),
			titleNorm: normalizeTitle(f.Title, f.Summary),
		}
	}

	seen := make(map[key]int)
	out := make([]CrossCheckFinding, 0, len(findings))

	for _, f := range findings {
		k := keyFor(f)
		// Findings with empty key (no RelatedIDs + no Type) always kept as-is
		// to avoid collapsing unrelated free-form findings.
		if k.idsKey == "" && k.typeName == "" {
			out = append(out, f)
			continue
		}
		if idx, ok := seen[k]; ok {
			existing := &out[idx]
			// Severity: blocking beats advisory.
			if f.Severity == "blocking" {
				existing.Severity = "blocking"
			}
			// InvolvedGroups: sorted union.
			existing.InvolvedGroups = unionSortedStrings(existing.InvolvedGroups, f.InvolvedGroups)
			// RelatedIDs: sorted union.
			existing.RelatedIDs = unionSortedInts(existing.RelatedIDs, f.RelatedIDs)
			// Title: keep the longer/more descriptive one; empty → take the new one.
			if existing.Title == "" {
				existing.Title = f.Title
			} else if f.Title != "" && utf8.RuneCountInString(f.Title) > utf8.RuneCountInString(existing.Title) {
				existing.Title = f.Title
			}
			// Summary: keep longer; if both non-empty and distinct, join them.
			if existing.Summary == "" {
				existing.Summary = f.Summary
			} else if f.Summary != "" && existing.Summary != f.Summary {
				existing.Summary = truncateByRunes(existing.Summary+"\n---\n"+f.Summary, 1000)
			}
			continue
		}
		seen[k] = len(out)
		f.InvolvedGroups = unionSortedStrings(nil, f.InvolvedGroups)
		out = append(out, f)
	}
	return out
}

// unionSortedInts returns a sorted slice containing all unique ints from a and b.
func unionSortedInts(a, b []int) []int {
	set := make(map[int]struct{}, len(a)+len(b))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		set[v] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]int, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func unionSortedStrings(a, b []string) []string {
	set := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		set[s] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
