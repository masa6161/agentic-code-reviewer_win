# CLAUDE.md - Development Guide

This file provides guidance for AI assistants working on the ACR codebase.
応答，コミットメッセージ，PR本文には日本語を使用すること

## Project Overview

ACR (Agentic Code Reviewer) is a Go CLI that runs parallel code reviews using LLM agents (Codex, Claude, or Gemini). It spawns N reviewers, collects their findings, deduplicates/clusters them via an LLM summarizer, and optionally posts results to GitHub PRs.

**Status note**: PR posting is beta on the Windows port. `--local` is the
supported path while the end-to-end submission flow is stabilized.

## Build & Test Commands

Use `make` for all build/test/lint operations. Run `make help` to see available targets.

```bash
make build       # Build with version info (outputs to bin/)
make check       # Run all quality checks (fmt, lint, vet, staticcheck, tests)
make test        # Run all tests
make lint        # Run golangci-lint v2
make staticcheck # Run staticcheck
make fmt         # Format code
make clean       # Clean build artifacts
```

Direct go commands (if needed):

```bash
go test ./...      # Run tests directly
go install ./cmd/acr  # Install locally
```

## Architecture

```
cmd/acr/                   # CLI entry point and subcommands
  main.go                  # CLI entry point, flag parsing, cobra root command
  review.go                # Core review orchestration (executeReview)
  review_opts.go           # ReviewOpts struct bundling resolved config + CLI flags
  pr_submit.go             # PR submission flow (post comments, approve)
  config_cmd.go            # `acr config` subcommand (init/show .acr.yaml)
  help.go                  # Custom help formatting (flag groups)
  helpers.go               # CLI helper functions (finding filtering, etc.)
  version.go               # Version info (injected via ldflags)
  signals_unix.go          # Unix signal handling (build-tagged)
  signals_windows.go       # Windows signal handling (build-tagged)
  *_test.go                # Tests for review, helpers, config_cmd, pr_submit, help

internal/
  agent/                   # LLM agent abstraction layer
    doc.go                 # Package documentation
    agent.go               # Agent interface (ExecuteReview, ExecuteSummary)
    codex.go               # Codex CLI agent implementation
    claude.go              # Claude CLI agent implementation
    gemini.go              # Gemini CLI agent implementation
    factory.go             # Agent and parser factory functions (registry)
    cohort.go              # Multi-agent name parsing, availability checks, distribution
    config.go              # ReviewConfig and SummaryConfig structs
    executor.go            # Subprocess execution with stderr capture
    cmd_reader.go          # io.Reader wrapper with process lifecycle management
    result.go              # ExecutionResult (io.ReadCloser + exit code + stderr)
    auth.go                # Authentication failure detection (exit codes, stderr patterns)
    diff.go                # Git diff/fetch delegations (backward-compat aliases)
    diff_review.go         # Diff-based review execution (shared by Claude/Gemini)
    parser.go              # ReviewParser and SummaryParser interfaces
    claude_review_parser.go    # Claude review output parser
    codex_review_parser.go     # Codex review output parser (JSONL)
    gemini_review_parser.go    # Gemini review output parser
    claude_summary_parser.go   # Claude summary output parser
    codex_summary_parser.go    # Codex summary output parser
    gemini_summary_parser.go   # Gemini summary output parser
    prompts.go             # Default review/summary prompts per agent
    nonfinding.go          # "No issues found" response detection
    reffile.go             # Temp file management for large diffs
    severity.go            # Severity extraction from finding text
    process_unix.go        # Unix process group handling (build-tagged)
    process_windows.go     # Windows process group handling (build-tagged)
    *_test.go              # Extensive test suite

  config/                  # Configuration file support
    config.go              # Load/parse .acr.yaml, LoadEnvState(), Resolve() precedence

  domain/                  # Core types: Finding, AggregatedFinding, GroupedFindings
    finding.go             # Finding types, aggregation logic, disposition tracking
    result.go              # ReviewerResult and ReviewStats
    exitcode.go            # Exit code constants (0=clean, 1=findings, 2=error, 130=interrupted)
    phase.go               # Phase constants (PhaseArch, PhaseDiff)

  filter/                  # Finding filtering
    filter.go              # Exclude findings by regex pattern matching

  fpfilter/                # False positive filtering
    filter.go              # LLM-based false positive detection and removal
    prompt.go              # FP filter prompt templates (including prior feedback)

  feedback/                # PR feedback summarization
    fetch.go               # Fetch PR description and comments via gh CLI
    summarizer.go          # LLM-based summarization of prior PR discussion
    prompt.go              # Feedback summarization prompt template

  runner/                  # Review execution engine
    runner.go              # Parallel reviewer orchestration
    report.go              # Report rendering (terminal + markdown)
    phase.go               # PhaseConfig and multi-phase review planning
    spec.go                # ReviewerSpec and distribution formatting

  summarizer/              # LLM-based finding summarization
    summarizer.go          # Orchestrates agent execution and output parsing
    crosscheck.go          # Cross-check validation of summarizer output

  github/                  # GitHub PR operations via gh CLI
    pr.go                  # Post comments, approve PRs, check CI status
    fork.go                # Fork reference resolution

  git/                     # Git operations
    worktree.go            # Temporary worktree management
    diff.go                # Diff generation, branch update, diff size classification
    diffsplit.go           # Split unified diffs into per-file sections
    remote.go              # Remote management (add, fetch, URL operations)

  terminal/                # Terminal UI
    spinner.go             # Progress spinner
    logger.go              # Styled logging
    colors.go              # ANSI color codes
    format.go              # Text formatting utilities
    selector.go            # Interactive TUI selector (bubbletea-based)

  modelconfig/             # Model configuration resolution
    resolver.go            # Resolve model + effort for (size, role, agent) tuples
```

## Key Design Decisions

1. **Multi-Agent Support**: Supports multiple LLM backends (Codex, Claude, Gemini) via the `Agent` interface. Each agent handles its own CLI invocation and output parsing. Adding new agents requires implementing `Agent`, `ReviewParser`, and `SummaryParser`.

2. **External Dependencies**: Uses LLM CLIs (`codex`, `claude`, `gemini`) for reviews and `gh` CLI for GitHub. All are exec'd as subprocesses - no SDK dependencies.

3. **Parallel Execution**: Reviewers run concurrently via goroutines. Results collected via channels with context cancellation support.

4. **Finding Aggregation**: Three-phase process:
   - First: Exact-match deduplication in `domain.AggregateFindings()`
   - Then: Semantic clustering via LLM in `summarizer.Summarize()`
   - Finally: LLM-based false positive filtering in `fpfilter.Filter()` (enabled by default, configurable threshold)

5. **Exit Codes**: Semantic exit codes (0=clean, 1=findings, 2=error, 130=interrupted) for CI integration.

6. **Terminal Detection**: Colors auto-disabled when stdout is not a TTY.

7. **Auto-phase (default on)**: ACR automatically chooses review phases based on diff size. Small diffs → flat diff-only review; large diffs → grouped arch+diff. Opt out with `--no-auto-phase`, `--phase small`, `.acr.yaml: auto_phase: false`, or `ACR_AUTO_PHASE=false`. See README "Auto-phase (default) vs flat review" for the full contract.

## Code Patterns

- **Error handling**: Return errors up the call stack. Log at the top level in main.go.
- **Context propagation**: All long-running operations accept `context.Context` for cancellation.
- **Configuration**: Three-tier precedence (flags > env vars > .acr.yaml > defaults). See `internal/config/config.go` for resolution logic.
- **Testing**: Table-driven tests preferred. See `internal/domain/finding_test.go` for examples.

## Adding New Features

When adding features:

1. **Domain types go in `internal/domain/`** - Keep them simple, no external dependencies.
2. **New CLI flags** - Add to `cmd/acr/main.go`, env var parsing in `internal/config/config.go`.
3. **Tests required** - Add `_test.go` files alongside implementation.
4. **Lint clean** - Run `make lint` before committing.

## Common Tasks

### Adding a new CLI flag

```go
// In cmd/acr/main.go, add to var block:
var myFlag string

// In run(), add flag definition with a hardcoded default:
rootCmd.Flags().StringVarP(&myFlag, "my-flag", "m", "default", "Description")

// In internal/config/config.go, add env var parsing in LoadEnvState():
if v := os.Getenv("ACR_MY_FLAG"); v != "" {
    state.MyFlag = v
}

// Precedence is resolved automatically in Resolve(): flags > env > .acr.yaml > defaults
```

### Adding a new finding field

1. Update `domain.Finding` struct
2. Update `domain.AggregatedFinding` if needed
3. Update aggregation logic in `domain.AggregateFindings()`
4. Update summarizer prompt if the field should be considered in clustering
5. Add tests

## Development Workflow: Binary Separation

During ACR feature development, two separate binaries are used:

| Purpose | Binary | Path |
|---|---|---|
| **Code review gate** | Stable installed binary | `C:\Users\kondo\go\bin\acr.exe` |
| **ACR dev testing** | Local test build | `.\acr.exe` (repo root) |

- **Code review**: Always use the installed stable binary (`C:\Users\kondo\go\bin\acr.exe`). Never use a test build for reviewing code.
- **Testing ACR changes**: Build with `go build -o .\acr.exe .\cmd\acr` and test locally.
- **Updating the stable binary**: Run `go install ./cmd/acr` only after ACR changes are merged to main.

See `AGENTS.md` section "ACR バイナリの使い分け" for detailed operational guidance.

## Release Process

Releases are automated via GoReleaser when tags are pushed:

```bash
git tag v1.2.3
git push origin v1.2.3
```

This triggers `.github/workflows/release.yml` which builds binaries for Linux/macOS (amd64/arm64), creates GitHub releases, and updates the Homebrew tap.
