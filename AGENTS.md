# AGENTS.md

このファイルはリポジトリ全体に適用される開発ガイドです。
すべての AI エージェント（Claude Code, Codex, Gemini, Copilot）が参照する共通ドキュメントです。

## 方針

- 応答、コミットメッセージ、PR 本文には日本語を使用すること。
- 既存設計を尊重し、最小差分で直す。
- 変更後は、触った領域に対応するテストとビルドを自分で実行して確認する。
- 長い運用メモをここに集約しすぎない。新しい知見は README / backlog / テストに反映する。

## プロジェクト概要

- このリポジトリは Go 製 CLI `acr` の **native Windows** ポーティングである。
- フォーク元である https://github.com/richhaase/agentic-code-reviewer は mac/unix 系のみを対象とした実装であり、これを native Windows 環境で動作するよう改変する。
- LLM reviewer CLI (`codex`, `claude`, `gemini`) を subprocess で起動する。
- GitHub 連携は `gh` CLI 前提。
- PR 投稿機能は Windows ポートではベータ段階。`--local` が現在サポートされるパス。

主要ディレクトリ:

- `cmd/acr/`: CLI エントリポイントと orchestration
- `internal/agent/`: reviewer / summarizer CLI 呼び出しと parser
- `internal/runner/`: 並列 reviewer 実行
- `internal/domain/`: コア型定義（Finding, AggregatedFinding, GroupedFindings）
- `internal/config/`: 設定ファイル（.acr.yaml）サポート
- `internal/summarizer/`, `internal/fpfilter/`: 要約と false positive filter
- `internal/feedback/`: PR フィードバック要約
- `internal/github/`, `internal/git/`: GitHub / Git 操作
- `internal/terminal/`: ターミナル UI
- `internal/modelconfig/`: モデル設定解決
- `integration/`: 実バイナリを使う integration test

## ビルドとテスト

Windows / PowerShell では直接 `go` コマンドを使用する。

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

リポジトリには `Makefile` が存在するが、Unix 系コマンド（`mkdir -p`, `date -u`, `rm -rf`）に依存しており、native Windows では動作しない。Windows では上記の `go` コマンドを直接使用すること。

auto-phase はデフォルト ON（差分サイズで自動的にフェーズを選択）。無効化は `--no-auto-phase`、`ACR_AUTO_PHASE=false`、または `.acr.yaml: auto_phase: false`。

## 設計方針

1. **マルチエージェント対応**: 複数の LLM バックエンド（Codex, Claude, Gemini）を `Agent` インターフェースで抽象化。各エージェントは独自の CLI 呼び出しと出力パースを担当する。新エージェント追加には `Agent`, `ReviewParser`, `SummaryParser` の実装が必要。

2. **外部依存**: LLM CLI (`codex`, `claude`, `gemini`) と `gh` CLI をサブプロセスとして実行。SDK 依存なし。

3. **並列実行**: レビュワーは goroutine で並行実行。チャネル経由で結果を収集し、context キャンセルに対応。

4. **Finding 集約**: 3 段階プロセス:
   - 完全一致の重複排除（`domain.AggregateFindings()`）
   - LLM によるセマンティッククラスタリング（`summarizer.Summarize()`）
   - LLM による誤検出フィルタリングと重要度トリアージ（`fpfilter.Filter()`）。blocking / advisory / noise に分類し、noise はデフォルト非表示（`--show-noise` で表示）。

5. **終了コード**: CI 統合用のセマンティック終了コード（0=問題なし, 1=指摘あり, 2=エラー, 130=中断）。

6. **ターミナル検出**: stdout が TTY でない場合、カラー出力を自動無効化。

7. **Auto-phase（デフォルト ON）**: 差分サイズに基づいてレビューフェーズを自動選択。小さい差分 → フラットな diff レビュー、大きい差分 → グループ化された arch+diff レビュー。無効化は `--no-auto-phase`、`--phase small`、`.acr.yaml: auto_phase: false`、または `ACR_AUTO_PHASE=false`。

## コードパターン

- **エラー処理**: コールスタックを遡ってエラーを返す。トップレベル（main.go）でログ出力。
- **Context 伝播**: 長時間実行操作はすべて `context.Context` を受け取り、キャンセルに対応。
- **設定の優先順位**: flags > 環境変数 > .acr.yaml > デフォルト値（`internal/config/config.go` 参照）。
- **テスト**: テーブル駆動テストを推奨（`internal/domain/finding_test.go` が参考例）。

## 機能追加ガイド

1. **ドメイン型は `internal/domain/` に配置** — シンプルに保ち、外部依存を持たせない。
2. **新 CLI フラグ** — `cmd/acr/main.go` に追加し、環境変数パースは `internal/config/config.go` に追加。
3. **テスト必須** — 実装と同じディレクトリに `_test.go` ファイルを追加。
4. **リント通過** — コミット前に `go vet ./...` を実行。

### CLI フラグの追加手順

新しいフラグは 2 ファイル・4 箇所に影響する:

```go
// 1. cmd/acr/main.go — 変数宣言と cobra フラグ登録
var myFlag string
rootCmd.Flags().StringVarP(&myFlag, "my-flag", "m", "default", "説明")

// 2. cmd/acr/main.go — FlagState + flagValues への接続（run() 内）
flagState := config.FlagState{
    // ...
    MyFlagSet: cmd.Flags().Changed("my-flag"),
}
flagValues := config.ResolvedConfig{
    // ...
    MyFlag: myFlag,
}

// 3. internal/config/config.go — ResolvedConfig, FlagState, EnvState 構造体への追加
//    および LoadEnvState() での環境変数パース:
if v := os.Getenv("ACR_MY_FLAG"); v != "" {
    state.MyFlag = v
    state.MyFlagSet = true
}

// 4. internal/config/config.go — Resolve() での優先順位ロジック:
if flagState.MyFlagSet {
    result.MyFlag = flagValues.MyFlag
} else if envState.MyFlagSet {
    result.MyFlag = envState.MyFlag
}
// config file と Defaults がベース値を提供
```

### Finding フィールドの追加手順

1. `domain.Finding` 構造体を更新
2. 必要に応じて `domain.AggregatedFinding` を更新
3. `domain.AggregateFindings()` の集約ロジックを更新
4. フィールドがクラスタリングに影響する場合、summarizer プロンプトを更新
5. テストを追加

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

## PR 作成先ポリシー

PR は常に `masa6161/agentic-code-reviewer_win` (origin) に対して作成すること。

- フォーク元 `richhaase/agentic-code-reviewer` (upstream) に PR を作成してはならない
- `gh pr create` 実行時は `--repo masa6161/agentic-code-reviewer_win` を明示指定する
- ユーザーから明示的にフォーク元への PR を指示された場合のみ、upstream を対象にしてよい

## コミット方針

- 変更は意味単位で分割する。
- 過去コミットの是正なら `fixup` を優先する。
- ユーザーが明示しない限り push しない。

## 作業ログ

- 長めの調査内容や次スレッドへの引継ぎは `backlog/` に Markdown で残す。
- ファイル名は `{YYYY-MM-DD}_{XX}.md` とし、`XX` は 01 からカウントアップさせる。
- 少なくとも次を含める:
  - 目的
  - 実際にしたこと
  - 未実行タスク
  - 次スレッドでやるべきこと
