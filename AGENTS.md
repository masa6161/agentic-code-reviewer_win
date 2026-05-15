# AGENTS.md

このファイルはリポジトリ全体に適用されるルールです。
すべての AI エージェント（Claude Code, Codex, Gemini, Copilot）が参照する共通ドキュメントです。
プロジェクト固有の設計方針や手順の詳細は [docs/development.md](docs/development.md) を参照してください。

## 方針

- 応答、コミットメッセージ、PR 本文には日本語を使用すること。
- 既存設計を尊重し、最小差分で直す。
- 変更後は、触った領域に対応するテストとビルドを自分で実行して確認する。

## 一般ルール

- Issue や PR の調査・分析を依頼された場合、明示的に指示されない限りコード変更を行わないこと。調査とは読み取り専用の探索を意味する：Read、Grep、分析のみ。
- プロジェクトファイル（バックログテンプレート、スキル定義、計画ファイル）を探す際は、現在のリポジトリの `.claude/` ディレクトリと同一階層のファイルを最初に確認すること。実際のコンテキストに存在しないファイル内容やコード詳細を捏造しないこと。

## プロジェクト概要

Go 製 CLI `acr` の native Windows ポーティング。フォーク元は [richhaase/agentic-code-reviewer](https://github.com/richhaase/agentic-code-reviewer)（mac/unix 専用）。LLM reviewer CLI (`codex`, `claude`, `gemini`) をサブプロセスで起動し、並列コードレビューを行う。GitHub 連携は `gh` CLI 前提。PR 投稿機能はベータ段階で、`--local` が現在サポートされるパス。

## ビルドとテスト

```powershell
go test ./...
go build ./cmd/acr
```

詳細（キャッシュ設定、auto-phase、Makefile の制限事項）は [docs/development.md](docs/development.md#ビルドとテスト詳細) を参照。

## 環境 / シェル

- 全シェルコマンドにデフォルトで PowerShell 構文を使用すること。bash 固有の構文（bash 式 sed、bash 環境変数展開など）は使用しない。
- git 操作の interactive rebase では `GIT_SEQUENCE_EDITOR` ハックより `--autosquash` フラグを優先。

## ACR バイナリの使い分け

- コードレビュー実行 → 安定バイナリ `C:\Users\kondo\go\bin\acr.exe`
- ACR のコード変更テスト → テストビルド `.\acr.exe`
- レビューゲートにテストビルドを使うことは禁止

詳細は [docs/development.md](docs/development.md#ACR-バイナリの使い分け詳細) を参照。

## 変更時の最低確認

- `internal/agent/` を触ったら: `go test ./internal/agent ./internal/runner ./integration`
- `cmd/acr/` を触ったら: `go build -o .\acr.exe .\cmd\acr` + 必要なら `.\acr.exe --help`
- reviewer 実行パスを触ったら: 可能なら `.\acr.exe --local ... --verbose` で実動作確認
- コードレビュー実行時: 必ず安定バイナリ `C:\Users\kondo\go\bin\acr.exe` を使用

## GitHub / Git 操作

- GitHub 操作には常にユーザーのフォークリポジトリ `masa6161/agentic-code-reviewer_win` (origin) を使用すること。upstream (`richhaase/agentic-code-reviewer`) は不可。`gh api` 呼び出し前にリモートターゲットを確認すること。
- `gh pr create` 実行時は `--repo masa6161/agentic-code-reviewer_win` を明示指定する。ユーザーから明示的にフォーク元への PR を指示された場合のみ、upstream を対象にしてよい。
- PR レビューワークフローでは、レビューコメントへの返信に正しいエンドポイントで `gh api` を使用すること（`POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}/comments` または pulls comments の返信エンドポイント）。呼び出し前に API エンドポイントを確認すること。
- 変更は意味単位で分割してコミットする。
- 過去コミットの是正なら `fixup` を優先する。
- ユーザーが明示しない限り push しない。

## 作業ログ

- 長めの調査内容や次スレッドへの引継ぎは `backlog/` に Markdown で残す。
- ファイル名は `{YYYY-MM-DD}_{XX}.md` とし、`XX` は 01 からカウントアップさせる。
- 少なくとも次を含める: 目的、実際にしたこと、未実行タスク、次スレッドでやるべきこと
