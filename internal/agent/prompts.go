package agent

import "strings"

// RenderPrompt replaces the {{guidance}} placeholder in a prompt template.
// If guidance is empty, the placeholder is stripped, producing clean output.
// If guidance is non-empty, the placeholder is replaced with an "Additional context:" section.
// If the template contains no placeholder, it is returned unchanged.
func RenderPrompt(template, guidance string) string {
	if guidance == "" {
		return strings.ReplaceAll(template, "{{guidance}}", "")
	}
	return strings.ReplaceAll(template, "{{guidance}}", "\n\nAdditional context:\n"+guidance)
}

// DefaultClaudePrompt is the default review prompt for Claude-based agents.
// This prompt instructs the agent to review code changes and output findings
// as simple text messages that will be aggregated and clustered.
const DefaultClaudePrompt = `Review this git diff for bugs.

Look for:
- Logic errors, wrong behavior, crashes
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions
- Missing operations (data not passed, steps skipped)

Skip:
- Style/formatting
- Performance unless severe
- Test files
- Suggestions

Output format: file:line: description
{{guidance}}`

// DefaultGeminiPrompt is the default review prompt for Gemini-based agents.
// Decoupled from Claude prompt to allow independent tuning.
const DefaultGeminiPrompt = `You are a code reviewer. Review the provided code changes (git diff) and identify actionable issues.

Focus on:
- Bugs and logic errors
- Security vulnerabilities (SQL injection, XSS, authentication issues, etc.)
- Performance problems (inefficient algorithms, resource leaks, unnecessary operations)
- Maintainability issues (code clarity, error handling, edge cases)
- Best practices violations for the language/framework being used

Output format:
- One finding per message
- Be specific: include file paths, line numbers, and exact issue descriptions
- Keep findings concise but complete (1-3 sentences)
- Only report actual issues - do not output "looks good" or "no issues found" messages
- If there are genuinely no issues, output nothing

Example findings:
- "auth/login.go:45: SQL injection vulnerability - user input not sanitized before query"
- "api/handler.go:123: Resource leak - HTTP response body not closed in error path"
- "utils/parser.go:67: Potential panic - missing nil check before dereferencing pointer"

Review the changes now and output your findings.
{{guidance}}`

// DefaultClaudeRefFilePrompt is the review prompt used when the diff is passed via
// a reference file instead of being embedded in the prompt. This avoids "prompt too long"
// errors for large diffs by having Claude read the diff using its file tools.
const DefaultClaudeRefFilePrompt = `Review this git diff for bugs.

The diff to review is in file: %s
Use the Read tool to examine it.

Look for:
- Logic errors, wrong behavior, crashes
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions
- Missing operations (data not passed, steps skipped)

Skip:
- Style/formatting
- Performance unless severe
- Test files
- Suggestions

Output format: file:line: description
{{guidance}}`

// DefaultGeminiRefFilePrompt is the review prompt used when the diff is passed via
// a reference file instead of being embedded in the prompt. This avoids prompt length
// errors for large diffs by having Gemini read the diff from the file.
const DefaultGeminiRefFilePrompt = `You are a code reviewer. Review the code changes in the diff file and identify actionable issues.

The diff to review is in file: %s
Read the file contents to examine the changes.

Focus on:
- Bugs and logic errors
- Security vulnerabilities (SQL injection, XSS, authentication issues, etc.)
- Performance problems (inefficient algorithms, resource leaks, unnecessary operations)
- Maintainability issues (code clarity, error handling, edge cases)
- Best practices violations for the language/framework being used

Output format:
- One finding per message
- Be specific: include file paths, line numbers, and exact issue descriptions
- Keep findings concise but complete (1-3 sentences)
- Only report actual issues - do not output "looks good" or "no issues found" messages
- If there are genuinely no issues, output nothing

Example findings:
- "auth/login.go:45: SQL injection vulnerability - user input not sanitized before query"
- "api/handler.go:123: Resource leak - HTTP response body not closed in error path"
- "utils/parser.go:67: Potential panic - missing nil check before dereferencing pointer"

Review the changes now and output your findings.
{{guidance}}`

// DefaultCodexPrompt is the default review prompt for Codex-based agents.
// Used when guidance is provided and we fall back to diff-based review
// because codex's --base flag and stdin prompt (-) are mutually exclusive (#170).
const DefaultCodexPrompt = `Review this git diff for bugs.

Look for:
- Logic errors, wrong behavior, crashes
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions
- Missing operations (data not passed, steps skipped)

Skip:
- Style/formatting
- Performance unless severe
- Test files
- Suggestions

Output format: file:line: description
{{guidance}}`

// DefaultCodexRefFilePrompt is the review prompt used when the diff is passed via
// a reference file instead of being embedded in the prompt.
const DefaultCodexRefFilePrompt = `Review this git diff for bugs.

The diff to review is in file: %s
Read the file contents to examine the changes.

Look for:
- Logic errors, wrong behavior, crashes
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions
- Missing operations (data not passed, steps skipped)

Skip:
- Style/formatting
- Performance unless severe
- Test files
- Suggestions

Output format: file:line: description
{{guidance}}`

// AutoPhaseDiffPrompt is the role-specific prompt for diff reviewers in auto-phase mode.
// Used when RolePrompts=true and Phase is set (non-empty). Agent-independent.
const AutoPhaseDiffPrompt = `Review this git diff for code-level issues.

You are reviewing a subset of files from a larger change.
Other reviewers are examining other files, and an architecture reviewer
has the full diff. Focus on issues detectable within these files.

Look for:
- Logic errors, wrong behavior, crashes, regressions
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions or boundary violations
- Missing operations (data not passed, steps skipped)
- Violations of existing coding patterns in the codebase

Skip:
- Cross-file architectural concerns (the arch reviewer handles those)
- Style/formatting
- Performance unless severe
- Test files
- Suggestions

Output format: file:line: description
{{guidance}}`

// AutoPhaseDiffRefFilePrompt is the ref-file variant of AutoPhaseDiffPrompt.
const AutoPhaseDiffRefFilePrompt = `Read the diff content from the file at %s and review it for code-level issues.

You are reviewing a subset of files from a larger change.
Other reviewers are examining other files, and an architecture reviewer
has the full diff. Focus on issues detectable within these files.

Look for:
- Logic errors, wrong behavior, crashes, regressions
- Security issues (injection, auth bypass, exposure)
- Silent failures, swallowed errors
- Wrong type conversions or boundary violations
- Missing operations (data not passed, steps skipped)
- Violations of existing coding patterns in the codebase

Skip:
- Cross-file architectural concerns (the arch reviewer handles those)
- Style/formatting
- Performance unless severe
- Test files
- Suggestions

Output format: file:line: description
{{guidance}}`

// AutoPhaseArchPrompt is the role-specific prompt for architecture reviewers in auto-phase mode.
// Used when RolePrompts=true and Phase=="arch". Agent-independent.
const AutoPhaseArchPrompt = `Review this code change for architectural and cross-cutting concerns.

You have the FULL diff of this change. Other reviewers are examining
individual file groups for code-level bugs. Focus on issues that span
multiple files or that individual file-level review would miss.

Focus on:
- Dependency direction violations (importing from wrong layer)
- Responsibility misplacement (logic in wrong package/module)
- Breaking changes to public interfaces or APIs
- Security design issues (auth bypass paths, trust boundary violations)
- Missing error propagation across module boundaries
- Cross-file consistency (e.g., a struct field added but not initialized in all constructors)
- Change coherence (do all modified files serve a single purpose?)

Output format:
- Prefix each issue with [must] for blocking or [imo] for advisory
- One issue per line

Skip:
- Implementation details within a single function (diff reviewers handle those)
- Style/formatting
- Performance micro-optimizations
{{guidance}}`

// AutoPhaseArchRefFilePrompt is the ref-file variant of AutoPhaseArchPrompt.
const AutoPhaseArchRefFilePrompt = `Read the diff content from the file at %s and review it for architectural and cross-cutting concerns.

You have the FULL diff of this change. Other reviewers are examining
individual file groups for code-level bugs. Focus on issues that span
multiple files or that individual file-level review would miss.

Focus on:
- Dependency direction violations (importing from wrong layer)
- Responsibility misplacement (logic in wrong package/module)
- Breaking changes to public interfaces or APIs
- Security design issues (auth bypass paths, trust boundary violations)
- Missing error propagation across module boundaries
- Cross-file consistency (e.g., a struct field added but not initialized in all constructors)
- Change coherence (do all modified files serve a single purpose?)

Output format:
- Prefix each issue with [must] for blocking or [imo] for advisory
- One issue per line

Skip:
- Implementation details within a single function (diff reviewers handle those)
- Style/formatting
- Performance micro-optimizations
{{guidance}}`

// DefaultArchPrompt is the default prompt for architecture-phase reviews.
// Used when ReviewConfig.Phase == "arch".
const DefaultArchPrompt = `Review this code change for architectural concerns.

Focus on:
- Dependency direction violations (importing from wrong layer)
- Responsibility misplacement (logic in wrong package/module)
- Breaking changes to public interfaces or APIs
- Security design issues (auth bypass paths, trust boundary violations)
- Missing error propagation across module boundaries

Output format:
- Prefix each issue with [must] for blocking or [imo] for advisory
- One issue per line

Skip:
- Implementation details within a single function
- Style/formatting
- Performance micro-optimizations
{{guidance}}`
