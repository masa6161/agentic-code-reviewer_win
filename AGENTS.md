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

## Windows のローカルバイナリ運用

重要:

- Windows で動作確認するときは、`PATH` 上の古い `acr.exe` を使わない。
- `acr` ではなく、必ずリポジトリ直下の `.\acr.exe` を明示実行する。

推奨手順:

```powershell
$env:GOCACHE="$PWD\.cache\go-build"
$env:GOMODCACHE="$PWD\.cache\gomod"
go build -o .\acr.exe .\cmd\acr
.\acr.exe --help
```

ローカルレビュー確認例:

```powershell
.\acr.exe --local --reviewers 3 --base HEAD~1 --reviewer-agent codex,claude,gemini --verbose
```

理由:

- `go install` 済みの `C:\Users\<user>\go\bin\acr.exe` が古いことがある。
- PowerShell では `acr` と打つとカレントディレクトリの `acr.exe` ではなく `PATH` 上の別バイナリが選ばれることがある。

## reviewer CLI 検証時の注意

- `codex` / `claude` / `gemini` の単独 CLI 動作確認と、`.\acr.exe` 経由の reviewer 実行確認は分けて考える。
- Windows では reviewer CLI が内部で追加 subprocess を spawn することがあるため、必要に応じて reviewer stderr を確認する。
- 3 エージェント並列レビューを試す場合は `--verbose` を付ける。

## 変更時の最低確認

- `internal/agent/` を触ったら:
  - `go test ./internal/agent ./internal/runner ./integration`
- `cmd/acr/` を触ったら:
  - `go build -o .\acr.exe .\cmd\acr`
  - 必要なら `.\acr.exe --help`
- reviewer 実行パスを触ったら:
  - 可能なら `.\acr.exe --local ... --verbose` で実動作確認

## コミット方針

- 変更は意味単位で分割する。
- 過去コミットの是正なら `fixup` を優先する。
- ユーザーが明示しない限り push しない。

## 作業ログ

- 長めの調査内容や次スレッドへの引継ぎは `back_log/` に Markdown で残す。
- ファイル名は`{YYYY-MM-DD}_{XX}.md`とし，`XX`は01からカウントアップさせる．
- 少なくとも次を含める:
  - 目的
  - 実際にしたこと
  - 未実行タスク
  - 次スレッドでやるべきこと
