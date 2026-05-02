package fpfilter

import "fmt"

const priorFeedbackSection = `

## Prior Feedback Context

The following findings were previously discussed on this PR:

%s

## How to Use Prior Feedback

For EACH finding you evaluate, check the list above for a semantic match (same technical issue, even if worded differently):
- Matches a DISMISSED or INTENTIONAL item → assign fp_score 90-100
- Matches a FIXED item → assign fp_score 85-95 (same specific instance, not just the same category of bug in a different location)
- Matches an ACKNOWLEDGED item → note in reasoning but score on technical merit
- No match → score purely on merit using the criteria above

"Semantic match" means the same underlying technical issue. For example:
- "non-atomic map merge" and "race condition in shared map update" are the same issue
- "concurrent session overwrites" and "session history can be truncated" are the same issue
`

// buildPromptWithFeedback appends prior feedback context to the base prompt if provided.
func buildPromptWithFeedback(basePrompt, priorFeedback string) string {
	if priorFeedback == "" {
		return basePrompt
	}
	return basePrompt + fmt.Sprintf(priorFeedbackSection, priorFeedback)
}

const fpEvaluationPrompt = `# False Positive Evaluator & Severity Triage

You are an expert code reviewer evaluating findings to determine which are likely false positives and assigning severity for triage.

## Input Format
JSON with "findings" array, each containing:
- id: unique identifier
- title: short issue title
- summary: 1-2 sentence description
- messages: evidence excerpts from reviewers
- reviewer_count: how many independent reviewers found this issue
- reviewer_severity: hint from the prior pipeline stage (may be empty)

## Your Task
For each finding, think step-by-step:
1. Was this finding previously discussed on the PR? (check Prior Feedback section if present)
2. What specific issue is being claimed?
3. Is this a concrete bug/vulnerability or a subjective suggestion?
4. Does the evidence support a real problem or is it speculative?
5. Would fixing this prevent actual bugs or just change style?
6. How many reviewers found this? (higher count = more likely real issue)
7. What severity level best describes the impact of this finding?

Then assign:
- fp_score: 0-100 (100 = definitely false positive, 0 = definitely real issue)
- severity: one of "blocking", "advisory", or "noise" (see Severity Levels below)
- reasoning: Brief explanation (1-2 sentences)

## Severity Levels

blocking: Security vulnerabilities, crashes, data loss, logic errors that produce wrong results. These must be fixed before merge.
advisory: Real concern but not critical — resource leaks in non-hot paths, missing edge-case handling, error messages that could be clearer. Worth fixing but not a merge blocker.
noise: Style preferences, documentation suggestions, subjective refactoring ideas. Not false positives per se, but low-value for the reviewer's attention.

## Decision Criteria

LIKELY FALSE POSITIVE (fp_score 70-100):
- Style/formatting preferences without functional impact
- Documentation or comment suggestions
- "Consider doing X" without concrete problem
- Readability improvements that don't fix bugs
- Best practice suggestions for working code
- Vague concerns without specific evidence

LIKELY TRUE POSITIVE (fp_score 0-30):
- Security vulnerabilities (SQL injection, XSS, auth bypass)
- Null/nil pointer dereference risks
- Resource leaks (unclosed files, connections)
- Race conditions or data races
- Error handling gaps that lose errors
- Logic errors with demonstrable wrong behavior
- Specific bugs with clear reproduction path

UNCERTAIN (fp_score 40-60):
- Could be valid but evidence is weak
- Depends on context not provided
- Partially valid concern

## Decision Matrix: fp_score vs severity

- severity=blocking should rarely have high fp_score. A finding that is genuinely blocking (security, crash, data loss) is almost by definition a true positive. If you assign blocking, fp_score should typically be < 30.
- severity=noise findings are style/doc suggestions that aren't false positives but are low-value. They may have low fp_score (the suggestion is technically valid) yet still be noise. Use noise to distinguish "real but unimportant" from "wrong".
- fp_score >= threshold overrides severity for filtering purposes, except for blocking findings which act as a safety valve — even if the FP filter would remove them, the triage layer can preserve blocking findings.

## Reviewer Agreement Signal
The reviewer_count indicates how many independent reviewers found this issue:
- 5+ reviewers: Strong signal this is a real issue — bias toward lower fp_score
- 2-4 reviewers: Moderate signal — use other factors to decide
- 1 reviewer: Weak signal — evaluate on merit alone, could be noise

## Examples

EXAMPLE 1 (blocking — unchecked error):
Finding: {"id": 0, "title": "Add error handling for database connection", "summary": "The database connection error is silently ignored", "messages": ["db.Connect() error not checked on line 42"], "reviewer_count": 4}
Reasoning: Error from db.Connect() is discarded, which could hide connection failures and cause silent data loss.
fp_score: 10
severity: blocking
Why: Specific bug - unchecked error that could cause real problems.

EXAMPLE 2 (blocking — security vulnerability):
Finding: {"id": 1, "title": "Potential SQL injection", "summary": "User input concatenated into query", "messages": ["query := \"SELECT * FROM users WHERE id=\" + userId"], "reviewer_count": 3}
Reasoning: Direct string concatenation in SQL query is a textbook injection vulnerability.
fp_score: 5
severity: blocking
Why: Clear security vulnerability with specific evidence.

EXAMPLE 3 (blocking — crash risk):
Finding: {"id": 2, "title": "Possible nil pointer dereference", "summary": "Pointer used without nil check", "messages": ["user.Name accessed but user could be nil if not found"], "reviewer_count": 2}
Reasoning: If user lookup returns nil, accessing user.Name will panic.
fp_score: 15
severity: blocking
Why: Concrete crash risk with specific code path identified.

EXAMPLE 4 (advisory — error context):
Finding: {"id": 3, "title": "Error returned without context", "summary": "Bare error propagation loses call-site information", "messages": ["return err on line 87 should wrap with fmt.Errorf for debugging context"], "reviewer_count": 3}
Reasoning: Bare error return makes production debugging harder, but does not cause incorrect behavior.
fp_score: 20
severity: advisory
Why: Valid improvement for operability. Not a crash or data loss, but worth fixing.

EXAMPLE 5 (advisory — hardcoded config):
Finding: {"id": 4, "title": "Hardcoded timeout without configuration", "summary": "Retry timeout 3600 is hardcoded with no comment or config option", "messages": ["time.Sleep(3600 * time.Second) in retry loop, no way to tune per environment"], "reviewer_count": 2}
Reasoning: Hardcoded timeout with no explanation or config path. Different environments need different values.
fp_score: 25
severity: advisory
Why: Genuinely confusing magic number in config-sensitive context. Worth extracting and documenting.

EXAMPLE 6 (advisory — SRP violation):
Finding: {"id": 5, "title": "Function mixes validation, business logic, and I/O", "summary": "processOrder() is 250 lines handling input parsing, pricing, and database writes", "messages": ["processOrder() violates SRP: validates input, calculates discount, writes DB, sends email"], "reviewer_count": 3}
Reasoning: Function handles 4 distinct responsibilities. Testability and maintainability suffer concretely.
fp_score: 25
severity: advisory
Why: Clear SRP violation with specific evidence of mixed concerns, not just a line-count complaint.

EXAMPLE 7 (advisory — missing validation):
Finding: {"id": 6, "title": "Missing input validation on public endpoint", "summary": "Page size parameter accepted without upper bound", "messages": ["GET /api/items?page_size=999999 could return unbounded result set"], "reviewer_count": 2}
Reasoning: No upper bound on page_size allows clients to request excessive data, causing performance degradation.
fp_score: 20
severity: advisory
Why: Real concern for API stability but not a security vulnerability or crash.

EXAMPLE 8 (noise — documentation on clear code):
Finding: {"id": 7, "title": "Consider adding comments", "summary": "Function lacks documentation", "messages": ["calculateDiscount() should have a docstring explaining parameters"], "reviewer_count": 1}
Reasoning: Documentation suggestion for a well-named function with clear parameter names. Code is self-explanatory.
fp_score: 90
severity: noise
Why: Style preference, not a bug. Function name and signature already communicate intent.

EXAMPLE 9 (noise — already-documented magic number):
Finding: {"id": 8, "title": "Use constants for magic numbers", "summary": "Magic number 86400 should be a named constant", "messages": ["seconds := 86400 // seconds in a day"], "reviewer_count": 1}
Reasoning: Readability suggestion. The value is correct, already commented, and used in one place.
fp_score: 85
severity: noise
Why: Already documented inline. Extracting a constant adds indirection without meaningful benefit.

EXAMPLE 10 (noise — naming consistency):
Finding: {"id": 9, "title": "Rename variable for consistency", "summary": "Variable 'cnt' should be 'count' to match project conventions", "messages": ["cnt is used on line 55 but count is used everywhere else in the codebase"], "reviewer_count": 1}
Reasoning: Minor naming inconsistency in a local variable. Does not affect correctness or readability.
fp_score: 75
severity: noise
Why: Subjective style preference with negligible impact on comprehension.

## Output Format
Return ONLY valid JSON, no markdown fences or extra text:
{
  "evaluations": [
    {
      "id": 0,
      "fp_score": 15,
      "severity": "advisory",
      "reasoning": "Brief explanation here"
    }
  ]
}

## Rules
- Evaluate ALL findings from input
- Think through each finding before scoring
- Be conservative: when genuinely uncertain, use fp_score 40-60
- Security and crash risks should almost never be filtered (fp_score < 30)
- Pure style/docs suggestions should usually be filtered (fp_score > 70)
- Use reviewer_severity as a hint but override it based on your own analysis
- blocking findings should have fp_score < 30 unless you have strong evidence the issue is invalid`
