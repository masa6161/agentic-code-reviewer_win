# AGENTS.md

このファイルの適用範囲は、このリポジトリ全体です。

## 方針

- 既存設計を尊重し、最小差分で直す。
- 変更後は、触った領域に対応するテストとビルドを自分で実行して確認する。
- 長い運用メモをここに集約しすぎない。新しい知見は README / back_log / テストに反映する。

## プロジェクト概要

- このリポジトリは Go 製 CLI `acr` の**native Windows**ポーティングである。
- フォーク元である https://github.com/richhaase/agentic-code-reviewer はmac/unix系のみを対象とした実装であり，これをnative windows環境で動作するよう改変する．
- LLM reviewer CLI (`codex`, `claude`, `gemini`) を subprocess で起動する。
- GitHub 連携は `gh` CLI 前提。

主要ディレクトリ:

- `cmd/acr/`: CLI エントリポイントと orchestration
- `internal/agent/`: reviewer / summarizer CLI 呼び出しと parser
- `internal/runner/`: 並列 reviewer 実行
- `internal/summarizer/`, `internal/fpfilter/`: 要約と false positive filter
- `integration/`: 実バイナリを使う integration test

## ビルドとテスト

- auto-phase はデフォルト ON（差分サイズで自動的にフェーズを選択）。無効化は `--no-auto-phase`、`ACR_AUTO_PHASE=false`、または `.acr.yaml: auto_phase: false`。

Unix 系では `make` が使えるが、Windows / PowerShell では直接 `go` コマンドを優先する。

よく使う確認:

```powershell
go test ./internal/agent ./internal/runner ./integration
go test ./...
go build ./cmd/acr
```

ローカルキャッシュ権限で失敗する環境では、リポジトリ内キャッシュを使う:

```powershell
$env:GOCACHE="$PWD\.cache\go-build"
$env:GOMODCACHE="$PWD\.cache\gomod"
go test ./internal/agent ./internal/runner ./integration
go build -o .\acr.exe .\cmd\acr
```

## ACR バイナリの使い分け

ACR 開発中は **2 つのバイナリ** を目的別に使い分ける。混同するとレビュー結果が不正確になるか、テストが古いコードに対して実行される。

### 1. レビューゲート用（安定バイナリ）

開発中のコードレビューには `go install` 済みの安定バイナリを使用する。

- パス: `C:\Users\kondo\go\bin\acr.exe`
- インストール方法: `go install ./cmd/acr`（main ブランチの安定状態で実行）
- 呼び出し: フルパス `C:\Users\kondo\go\bin\acr.exe` を明示指定する
- 更新タイミング: ACR 自体の機能変更がマージされた後に再インストール

レビュー実行例:

```powershell
C:\Users\kondo\go\bin\acr.exe --local --base HEAD~1 --verbose
```

### 2. ACR 開発テスト用（テストビルド）

ACR のソースコード自体を変更したとき、動作確認にはリポジトリ直下にビルドした `.\acr.exe` を使う。

```powershell
$env:GOCACHE="$PWD\.cache\go-build"
$env:GOMODCACHE="$PWD\.cache\gomod"
go build -o .\acr.exe .\cmd\acr
.\acr.exe --help
```

テストビルド動作確認例:

```powershell
.\acr.exe --local --reviewers 3 --base HEAD~1 --reviewer-agent codex,claude,gemini --verbose
```

### なぜ分けるのか

- テストビルド `.\acr.exe` は開発中のコードを含むため、壊れている可能性がある
- レビューゲートが壊れたバイナリを使うとレビュー結果が信頼できなくなる
- `go install` 済みバイナリは main ブランチの安定コミットに基づくため信頼性が高い

## reviewer CLI 検証時の注意

- `codex` / `claude` / `gemini` の単独 CLI 動作確認と、`.\acr.exe` 経由の reviewer 実行確認は分けて考える。
- Windows では reviewer CLI が内部で追加 subprocess を spawn することがあるため、必要に応じて reviewer stderr を確認する。
- 3 エージェント並列レビューを試す場合は `--verbose` を付ける。

## 変更時の最低確認

- `internal/agent/` を触ったら:
  - `go test ./internal/agent ./internal/runner ./integration`
- `cmd/acr/` を触ったら:
  - `go build -o .\acr.exe .\cmd\acr`（テストビルド）
  - 必要なら `.\acr.exe --help`
- reviewer 実行パスを触ったら:
  - 可能なら `.\acr.exe --local ... --verbose` で実動作確認（テストビルド使用）
- コードレビューを実行するとき:
  - 必ず安定バイナリ `C:\Users\kondo\go\bin\acr.exe` を使用する
  - テストビルド `.\acr.exe` をレビューゲートに使わない

## コミット方針

- 変更は意味単位で分割する。
- 過去コミットの是正なら `fixup` を優先する。
- ユーザーが明示しない限り push しない。

## 作業ログ

- 長めの調査内容や次スレッドへの引継ぎは `backlog/` に Markdown で残す。
- ファイル名は`{YYYY-MM-DD}_{XX}.md`とし，`XX`は01からカウントアップさせる．
- 少なくとも次を含める:
  - 目的
  - 実際にしたこと
  - 未実行タスク
  - 次スレッドでやるべきこと
