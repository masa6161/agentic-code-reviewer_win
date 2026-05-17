# ARC - Adaptive code-Review Coordinator

A CLI tool that runs parallel AI-powered code reviews using LLM agents ([Codex](https://github.com/openai/codex), [Claude Code](https://github.com/anthropics/claude-code), or [Gemini CLI](https://github.com/google-gemini/gemini-cli)) and aggregates findings intelligently.

ARC is a hard fork of Rich Haase's original Agentic Code Reviewer project, renamed and adapted as Adaptive Review Coordinator for the Windows-native path.

<!-- Uncomment after recording the demo:
<p align="center">
  <img src="docs/assets/demo.svg" alt="ARC demo" width="800">
</p>
-->

## Quick Start

```bash
# Install ARC
go install github.com/masa6161/arc-cli/cmd/arc@latest

# Install at least one LLM CLI (Codex shown here)
brew install codex

# Run a review in your repo
cd your-repo
arc
```

On Windows, use the source install or the release ZIP described below.

## Prerequisites

You need **at least one** of the following LLM CLIs installed and authenticated:

| Agent | Installation |
|-------|--------------|
| Codex | [github.com/openai/codex](https://github.com/openai/codex) (default) |
| Claude Code | [github.com/anthropics/claude-code](https://github.com/anthropics/claude-code) |
| Gemini CLI | [github.com/google-gemini/gemini-cli](https://github.com/google-gemini/gemini-cli) |

Optional:

| Tool | Installation | Purpose |
|------|--------------|---------|
| gh CLI | [cli.github.com](https://cli.github.com) | Post reviews to GitHub PRs |

## How It Works

ARC spawns multiple parallel reviewers, each invoking your chosen LLM agent (Codex, Claude, or Gemini) independently. The parallel approach increases coverage: different reviewers may catch different issues. After all reviewers complete, ARC aggregates and clusters similar findings using an LLM summarizer, filters out likely false positives, then presents a consolidated report.

```mermaid
graph TD
    A[arc] -->|spawns N reviewers| B
    subgraph Parallel Review
        B[Reviewer 1]
        C[Reviewer 2]
        D[Reviewer N]
    end
    A -->|if PR detected| P[PR Feedback Summarizer]
    P -->|summarizes prior discussion| F
    B & C & D --> E[Summarizer]
    E -->|clusters & deduplicates| F[FP Filter]
    F -->|removes false positives| G[Consolidated Report]
    G --> H[Terminal Report]
    H --> I[Done]
```

## Installation

### Source (macOS / Linux)

```bash
go install github.com/masa6161/arc-cli/cmd/arc@latest
```

### Windows

ARC works on Windows native with `codex`, `claude`, or `gemini` as the review backend.

#### From Source

```powershell
go install github.com/masa6161/arc-cli/cmd/arc@latest
arc --help
```

#### Direct Download

Download the Windows release ZIP from GitHub Releases and extract `arc.exe`.

## Usage

```bash
# Review current branch against main with 5 parallel reviewers
arc

# Review with custom settings
arc --reviewers 10 --base develop --timeout 10m

# Review a PR by number
arc --pr 123

# Review a specific branch in a temporary worktree
arc --worktree-branch feature/my-branch

# Review a PR from a forked repository
arc --worktree-branch username:feature-branch

# Verbose mode (show reviewer messages as they arrive)
arc --verbose
```

### Auto-phase (default) vs flat review

By default, ARC automatically selects review phases based on diff size ("auto-phase"). Large diffs are split into an architecture review plus per-file-group diff reviews; small diffs use a single flat diff pass.

To run a flat (single big diff × N reviewers) review instead:

| Method | How |
|---|---|
| Ad-hoc flag | `arc --phase small` |
| Disable auto-phase for one run | `arc --no-auto-phase` |
| Persistent opt-out in project | `.arc.yaml`: `auto_phase: false` |
| Persistent opt-out via env | `ARC_AUTO_PHASE=false arc` |

Using `--phase medium` forces both phases explicitly (arch + diff) without grouping, regardless of diff size.

The verdict field (`ok` / `advisory` / `blocking`) and exit-code policy apply on both paths. Use `--strict` to treat advisory findings as blocking (exit 1).

### Options

| Flag                | Short | Default | Description                              |
| ------------------- | ----- | ------- | ---------------------------------------- |
| `--reviewers`       | `-r`  | 5       | Number of parallel reviewers             |
| `--concurrency`     | `-c`  | -r      | Max concurrent reviewers (see below)     |
| `--base`            | `-b`  | main    | Base ref for diff comparison             |
| `--timeout`         | `-t`  | 10m     | Timeout per reviewer                     |
| `--retries`         | `-R`  | 1       | Retry failed reviewers N times           |
| `--verbose`         | `-v`  | false   | Print agent messages in real-time        |
| `--worktree-branch` | `-B`  |         | Review a branch in a temp worktree (supports `user:branch` for forks) |
| `--fetch/--no-fetch`|       | true    | Fetch base ref from origin before diff   |
| `--no-fp-filter`    |       | false   | Disable false positive filtering          |
| `--fp-threshold`    |       | 75      | False positive confidence threshold 1-100 |
| `--no-pr-feedback`  |       | false   | Disable PR feedback summarization         |
| `--pr-feedback-agent`|      |         | Agent for PR feedback summarization       |
| `--pr`              |       |         | Review a PR by number (fetches into temp worktree) |
| `--guidance`        |       |         | Steering context appended to review prompt (env: ARC_GUIDANCE) |
| `--guidance-file`   |       |         | Path to file containing review guidance (env: ARC_GUIDANCE_FILE) |
| `--ref-file`        |       | false   | Write diff to temp file instead of embedding in prompt (auto for large diffs) |
| `--exclude-pattern` |       |         | Exclude findings matching regex (repeat)  |
| `--no-config`       |       | false   | Skip loading .arc.yaml config file        |
| `--reviewer-agent`  | `-a`  | codex   | Agent(s) for reviews, comma-separated (codex, claude, gemini) |
| `--arch-reviewer-agent`|    |         | Single agent for arch phase in auto-phase grouped diff (default: first --reviewer-agent) |
| `--diff-reviewer-agents`|   |         | Agent(s) for diff phase in auto-phase grouped diff, comma-separated (default: same as --reviewer-agent) |
| `--summarizer-agent`| `-s`  | codex   | Agent for summarization (codex, claude, gemini) |
| `--reviewer-model`  |       |         | LLM model for review agents (env: ARC_REVIEWER_MODEL) |
| `--summarizer-model`|       |         | LLM model for summarizer/FP filter agents (env: ARC_SUMMARIZER_MODEL) |
| `--auto-phase`/`--no-auto-phase`| | true | Auto-select review phases based on diff size (env: ARC_AUTO_PHASE) |
| `--phase`           |       |         | Override auto-phase: small, medium, large  |
| `--large-diff-reviewers`|   | 4       | Number of diff reviewers in auto-phase grouped path (large diff) |
| `--medium-diff-reviewers`|  | 2       | Number of diff reviewers for auto-phase medium and --phase medium |
| `--small-diff-reviewers`|   | 1       | Number of reviewers for auto-phase small and --phase small |
| `--role-prompts`/`--no-role-prompts`| | true | Use role-specific prompts for auto-phase diff/arch reviewers (note: `--help` shows `false` as the cobra default, but the effective default is `true` via config resolution) |
| `--summarizer-timeout`|     | 5m      | Timeout for summarizer phase              |
| `--fp-filter-timeout`|      | 5m      | Timeout for false positive filter phase   |
| `--no-cross-check`  |       | false   | Disable cross-group consistency verification |
| `--cross-check-agent`|      |         | Agent(s) for cross-check verification, comma-separated (default: same as --summarizer-agent) |
| `--cross-check-model`|      |         | LLM model(s) for cross-check, comma-separated (REQUIRED when cross-check enabled) |
| `--cross-check-timeout`|    | 5m      | Timeout for cross-check phase             |
| `--strict`          |       | false   | Exit 1 on any advisory verdict            |
| `--format`          |       | text    | Output format: text or json               |

### Concurrency Control

The `--concurrency` flag limits how many reviewers run simultaneously, independent of the total reviewer count. This helps avoid API rate limits when running many reviewers or using high retry counts.

```bash
# Run 15 total reviewers, but only 5 at a time
arc -r 15 -c 5

# With retries, -c prevents retry storms from overwhelming the API
arc -r 10 -R 3 -c 3
```

By default, concurrency equals the reviewer count (all run in parallel).

### Fork PR Support

Review pull requests from forked repositories using GitHub's `username:branch` notation:

```bash
# Review a PR from user "contributor" on branch "fix-bug"
arc --worktree-branch contributor:fix-bug
```

ARC will:
1. Query GitHub to find the open PR from that user's branch
2. Add a temporary remote pointing to the fork
3. Fetch the branch
4. Create a worktree and run the review
5. Clean up the temporary remote

This requires an open PR from the fork to the current repository. The `gh` CLI must be authenticated.

### Agent Selection

ARC supports multiple AI backends for code review:

| Agent | CLI | Description |
|-------|-----|-------------|
| `codex` | [Codex](https://github.com/openai/codex) | Default. Uses built-in `codex exec review` |
| `claude` | [Claude Code](https://github.com/anthropics/claude-code) | Anthropic's Claude via CLI |
| `gemini` | [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Google's Gemini via CLI |

```bash
# Use Claude instead of Codex for reviews
arc --reviewer-agent claude

# Use Gemini for reviews
arc -a gemini

# Use different agents for review and summarization
arc --reviewer-agent gemini --summarizer-agent claude

# Use multiple agents in round-robin (reviewers alternate between agents)
arc -r 6 --reviewer-agent codex,claude,gemini

# Override the model used by review agents
arc --reviewer-agent claude --reviewer-model sonnet-4

# Use different models for review and summarization
arc --reviewer-agent claude --reviewer-model opus-4 \
    --summarizer-agent claude --summarizer-model haiku-4
```

Different agents may find different issues. When multiple agents are specified (comma-separated), reviewers are assigned to agents in round-robin order. The appropriate CLI must be installed and authenticated for all selected agents.

### Review Guidance

Steer reviews with additional context without replacing the built-in prompts:

```bash
# Inline guidance
arc --guidance "Focus on security vulnerabilities and auth issues"

# Guidance from file
arc --guidance-file .arc-guidance.md
```

Guidance is appended to the default review prompts, preserving the tuned output format and skip rules. Use it to provide domain context, focus areas, or project conventions.

### PR Feedback Summarization

When reviewing a PR (via `--pr` flag or auto-detected from the current branch), ARC can summarize prior PR discussion to improve false positive filtering. This helps avoid re-surfacing issues that have already been discussed and dismissed.

The summarizer fetches:
- PR description
- Review comments (inline code comments)
- Issue comments (general PR discussion)
- Review summaries (approve/request-changes/comment bodies)

This context is passed to the false positive filter, which can then recognize findings that were previously acknowledged as intentional or already addressed.

```bash
# Disable PR feedback summarization
arc --no-pr-feedback

# Use a specific agent for feedback summarization
arc --pr-feedback-agent claude
```

PR feedback summarization runs in parallel with the reviewers and is enabled by default. It only activates when:
1. A PR is detected (via `--pr` flag or auto-detection)
2. The false positive filter is enabled

### Environment Variables

| Variable                  | Description                              |
| ------------------------- | ---------------------------------------- |
| `ARC_REVIEWERS`           | Default number of reviewers              |
| `ARC_CONCURRENCY`         | Default max concurrent reviewers         |
| `ARC_BASE_REF`            | Default base ref                         |
| `ARC_TIMEOUT`             | Default timeout (e.g., "5m" or "300")    |
| `ARC_RETRIES`             | Default retry count                      |
| `ARC_FETCH`               | Fetch base ref from origin (true/false)  |
| `ARC_FP_FILTER`           | Enable false positive filtering (true/false) |
| `ARC_FP_THRESHOLD`        | False positive confidence threshold 1-100 |
| `ARC_PR_FEEDBACK`         | Enable PR feedback summarization (true/false) |
| `ARC_PR_FEEDBACK_AGENT`   | Agent for PR feedback summarization |
| `ARC_REVIEWER_AGENT`      | Default reviewer agent(s), comma-separated |
| `ARC_ARCH_REVIEWER_AGENT` | Single agent for arch phase in auto-phase grouped diff |
| `ARC_DIFF_REVIEWER_AGENTS`| Agent(s) for diff phase in auto-phase grouped diff |
| `ARC_SUMMARIZER_AGENT`    | Default summarizer agent  |
| `ARC_CODEX_HOME`          | Codex home directory passed to `codex` subprocesses; set as a user environment variable, not in `.arc.yaml` |
| `CODEX_HOME`              | Fallback Codex home directory when `ARC_CODEX_HOME` is unset |
| `ARC_SUMMARIZER_TIMEOUT`  | Timeout for summarizer phase (e.g., "5m" or "300") |
| `ARC_FP_FILTER_TIMEOUT`   | Timeout for false positive filter phase (e.g., "5m" or "300") |
| `ARC_AUTO_PHASE`          | Enable auto-phase selection (true/false)   |
| `ARC_LARGE_DIFF_REVIEWERS`| Number of diff reviewers for auto-phase large path |
| `ARC_MEDIUM_DIFF_REVIEWERS`| Number of diff reviewers for auto-phase medium path |
| `ARC_SMALL_DIFF_REVIEWERS`| Number of reviewers for auto-phase small path |
| `ARC_ROLE_PROMPTS`        | Enable role-specific prompts (true/false)  |
| `ARC_CROSS_CHECK`         | Enable cross-group consistency verification (true/false) |
| `ARC_CROSS_CHECK_AGENT`   | Agent(s) for cross-check verification      |
| `ARC_CROSS_CHECK_MODEL`   | LLM model(s) for cross-check               |
| `ARC_CROSS_CHECK_TIMEOUT` | Timeout for cross-check phase (e.g., "5m" or "300") |
| `ARC_STRICT`              | Exit 1 on any advisory verdict (true/false) |
| `ARC_GUIDANCE`            | Steering context appended to review prompt |
| `ARC_GUIDANCE_FILE`       | Path to file containing review guidance    |

Codex auth home is intentionally operator-controlled. On Windows, prefer a User
environment variable and restart the terminal/agent process so child `codex`
processes inherit it:

```powershell
[Environment]::SetEnvironmentVariable("ARC_CODEX_HOME", "C:\Users\<you>\.arc-codex-home", "User")
```

## Configuration

Create `.arc.yaml` in your repository root to configure persistent settings:

```yaml
# All fields are optional - defaults shown in comments
reviewers: 5              # Number of parallel reviewers
concurrency: 5            # Max concurrent reviewers (defaults to reviewers)
base: main                # Base ref for diff comparison
timeout: 10m              # Timeout per reviewer (supports "5m", "300s", or 300)
retries: 1                # Retry failed reviewers N times
fetch: true               # Fetch base ref from origin before diff

# Agent selection
# reviewer_agent: codex   # Single agent for reviews (codex, claude, gemini)
# reviewer_agents:        # Multiple agents for round-robin assignment
#   - codex
#   - claude
#   - gemini
# summarizer_agent: codex # Agent for summarization (codex, claude, gemini)
# Codex home is intentionally not configurable in .arc.yaml because repository
# config is shared. Set ARC_CODEX_HOME as a user environment variable instead.
# Precedence: ARC_CODEX_HOME > CODEX_HOME > USERPROFILE/HOME/.codex
# reviewer_model: ""      # LLM model override for review agents
# summarizer_model: ""    # LLM model override for summarizer/FP filter agents
summarizer_timeout: 5m    # Timeout for summarizer phase
fp_filter_timeout: 5m     # Timeout for false positive filter phase

# Review guidance (appended to built-in prompts)
# guidance_file: .arc-guidance.md

filters:
  exclude_patterns:       # Regex patterns to exclude from findings
    - "Next\\.js forbids"
    - "deprecated API"
    - "consider using"

fp_filter:
  enabled: true           # Enable LLM-based false positive filtering
  threshold: 75           # Confidence threshold 1-100 (100 = definitely false positive)

pr_feedback:
  enabled: true           # Summarize prior PR comments to improve FP filtering
  # agent: claude         # Agent for summarization (defaults to summarizer_agent)
```

### Model matrix (optional)

Per-role, per-size, and per-agent model/effort overrides can be set under a `models:` key. All three layers are optional and cascade: `agents` > `sizes` > `defaults` > legacy flat fields (`reviewer_model`, `summarizer_model`, etc.) > agent built-in default.

```yaml
# Optional: per-size / per-role / per-agent model and thinking-effort matrix.
# All three layers are optional and cascade: agents > sizes > defaults > legacy.
# Unset fields fall back to lower layers, then to the agent's built-in default.
models:
  defaults:
    reviewer:       { model: gpt-5.4-mini, effort: medium }
    arch_reviewer:  { model: gpt-5.4,      effort: high }   # auto-phase arch only
    diff_reviewer:  { model: gpt-5.4-mini, effort: medium } # auto-phase diff only
    summarizer:     { model: gpt-5.4,      effort: high }
    fp_filter:      { model: gpt-5.4-mini, effort: low }
    cross_check:    { model: gpt-5.4,      effort: medium }
    pr_feedback:    { model: gpt-5.4-mini }
  sizes:
    large:
      arch_reviewer: { model: gpt-5.4,     effort: high }
      diff_reviewer: { model: gpt-5.4,     effort: medium }
      summarizer:    { model: gpt-5.4,     effort: high }
  agents:
    codex:
      arch_reviewer: { effort: high }
      diff_reviewer: { effort: medium }
    claude:
      reviewer:   { model: sonnet-4-6,   effort: high }
```

**`arch_reviewer` / `diff_reviewer` phase-specific roles**: when auto-phase enters a multi-phase run (`medium`/`large` diffs → `arch` + `diff` phases), the phase-specific reviewer role is consulted first at each cascade layer, falling back to the generic `reviewer` role at the *same* layer before descending. Flat review paths (auto-phase OFF, `size=small`, or explicit `--phase small`) ignore these keys and use `reviewer` only. Legacy flat fields (`reviewer_model`, etc.) apply as the generic reviewer fallback only — there is no `arch_reviewer_model` legacy field.

**`effort` field behavior by agent:**
- **codex**: `low`, `medium`, or `high` map to `-c model_reasoning_effort=<value>` (codex CLI's config override; there is no `--reasoning-effort` flag). Unknown values are ignored.
- **claude**: `low`, `medium`, `high`, `xhigh`, or `max` map to `--effort <value>` (session-scoped; available levels depend on the model). Other values (including numeric strings) are silently dropped.
- **gemini**: effort is not supported and is silently ignored.

When `models:` is absent, the legacy flat fields (`reviewer_model`, `summarizer_model`, etc.) remain fully functional as before. Use `--verbose` to see the resolved effective matrix at runtime.

### Precedence

Configuration is resolved with the following precedence (highest to lowest):
1. CLI flags (e.g., `--reviewers 10`)
2. Environment variables (e.g., `ARC_REVIEWERS=10`)
3. `models.agents.<agent>.<role>` in `.arc.yaml`
4. `models.sizes.<size>.<role>` in `.arc.yaml`
5. `models.defaults.<role>` in `.arc.yaml`
6. Legacy flat fields (`reviewer_model`, `summarizer_model`, etc.) in `.arc.yaml`
7. Built-in defaults

### Behavior

- Config file is loaded from the git repository root
- Missing config file is not an error (empty defaults used)
- Invalid YAML or regex patterns produce an error
- Unknown keys in config file produce a warning with "did you mean?" suggestions
- CLI `--exclude-pattern` flags are merged with config patterns (union)
- Use `--no-config` to skip loading the config file for a single run

## Exit Codes

| Code | Meaning                      |
| ---- | ---------------------------- |
| 0    | No findings                  |
| 1    | Findings found               |
| 2    | Error                        |
| 130  | Interrupted (SIGINT/SIGTERM) |

## Development

### Unix / macOS

```bash
# List available targets
make help

# Build with version info (outputs to bin/)
make build

# Run all quality checks (format, lint, vet, staticcheck, tests)
make check

# Run tests
make test

# Run linter
make lint

# Run staticcheck
make staticcheck

# Format code
make fmt

# Clean build artifacts
make clean
```

### Windows (PowerShell)

```powershell
go test ./...
go build ./cmd/arc
go install ./cmd/arc
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

## License

Apache License 2.0 - see [LICENSE](LICENSE)
