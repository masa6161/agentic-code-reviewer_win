# Contributing to ARC

Contributions are greatly appreciated! Please note that all contributions
are reviewed at the maintainer's discretion — submitting a PR does not
obligate acceptance.

## Prerequisites

- Go 1.25+
- At least one LLM CLI (codex, claude, or gemini) installed and authenticated
- gh CLI (for integration testing)

## Development Workflow

1. Fork and clone the repo
2. Create a feature branch
3. Make your changes
4. Run `make check` (must pass — covers fmt, lint, vet, staticcheck, tests)
5. Open a PR

## PR Requirements

All PRs must include evidence of a successful ARC run against the
contributed code using the repository's `.arc.yaml` configuration
(which uses all three agent types with 6 reviewers):

    arc --pr <your-pr-number>

If you don't have access to all three agents (codex, claude, gemini),
you must review with at least 2. Override with:

    arc --pr <your-pr-number> --reviewer-agent codex,claude

## Project Structure

アーキテクチャ概要、コードパターン、機能追加ガイドは [AGENTS.md](AGENTS.md) を参照。

## AI コントリビューター

本プロジェクトでは [AGENTS.md](AGENTS.md) を全 AI エージェント共通の開発ガイドとして使用しています。
Claude Code, Codex, Gemini 等のツールで貢献する場合は、このファイルを参照してください。
