# agentic-code-reviewer 技術調査レポート

> リポジトリ: https://github.com/richhaase/agentic-code-reviewer
> 調査日: 2026-04-11

## 1. プロジェクト概要

- **言語**: Go
- **目的**: 複数の AI Coding Agent CLI を並列実行し、コードレビューの findings を集約・要約・偽陽性フィルタリングするツール
- **対応バックエンド**: Codex (OpenAI), Claude (Anthropic), Gemini (Google)
- **CLI フレームワーク**: spf13/cobra
- **設定ファイル**: `.acr.yaml` (gopkg.in/yaml.v3)

## 2. アーキテクチャ全体像

```
N個のレビュアーを並列実行 (runner)
  │  各レビュアーの findings を集約
  ▼
Finding Summarizer (LLM でクラスタリング・要約)
  │
  │  並行: Feedback Summarizer (PR の既存コメントを構造化)
  ▼
FP Filter (LLM で fp_score 判定 + agreement bonus)
  │  threshold 以上を除外
  ▼
最終レポート出力 / PR コメント投稿
```

LLM API を直接呼び出すのではなく、`exec.CommandContext` 経由で各 CLI をサブプロセスとして起動し、標準入出力をパイプする設計。

## 3. 対応エージェントと CLI 呼び出し

### 3.1 レビュー実行時

| エージェント | CLI コマンド | 引数 |
|---|---|---|
| codex (デフォルト) | `codex exec --json --color never review --base <ref>` | guidance 無し時。guidance 有り時は `codex exec --json --color never -` でstdin入力 |
| claude | `claude --print -` | stdin にプロンプト+diff を流す |
| gemini | `gemini -o json -` | stdin にプロンプト+diff を流す |

### 3.2 サマリ / FP フィルタ実行時

| エージェント | CLI コマンド | 備考 |
|---|---|---|
| codex | `codex exec --json --color never -` | stdin にプロンプト+JSON |
| claude | `claude --print --output-format json -` | 100KB超は一時ファイル+Read tool |
| gemini | `gemini -o json -` | stdin にプロンプト+JSON |

すべてのエージェントで `--model` オプションによるモデルオーバーライドが可能。

### 3.3 エージェントレジストリ

`internal/agent/factory.go` にレジストリパターンで管理：

```go
var registry = map[string]agentRegistry{
    "codex":  { newAgent, newReviewParser, newSummaryParser },
    "claude": { newAgent, newReviewParser, newSummaryParser },
    "gemini": { newAgent, newReviewParser, newSummaryParser },
}
```

新規エージェント追加は1箇所の登録で完結する。

## 4. False Positive Filter (`internal/fpfilter/`)

### 4.1 動作原理

LLM に「偽陽性評価者」としてプロンプトを送り、各 finding に `fp_score` (0-100) を付けさせる。

- `fp_score >= threshold (デフォルト75)` → false positive として除去
- fail-open 設計: LLM 実行失敗・パース失敗時はフィルタをスキップし全 finding を通す

### 4.2 agreement bonus（ヒューリスティック補正）

複数レビュアのうち報告比率が低い finding には fp_score にボーナスを加算：

```go
func agreementBonus(reviewerCount, totalReviewers int) int {
    ratio := float64(reviewerCount) / float64(totalReviewers)
    switch {
    case ratio < 0.2: return 15  // 20%未満の合意 → +15
    case ratio < 0.4: return 10  // 40%未満の合意 → +10
    default:          return 0
    }
}
```

### 4.3 prior feedback 統合

PR上の過去コメント（DISMISSED / FIXED / INTENTIONAL / ACKNOWLEDGED）をプロンプトに注入し、既知の議論済み項目のスコアを引き上げる。

### 4.4 プロンプトの判定基準

| 分類 | fp_score | 具体例 |
|---|---|---|
| LIKELY FALSE POSITIVE | 70-100 | スタイル/フォーマット、ドキュメント提案、"Consider doing X" |
| UNCERTAIN | 40-60 | エビデンス不足、コンテキスト依存 |
| LIKELY TRUE POSITIVE | 0-30 | セキュリティ脆弱性、null参照、リソースリーク、競合状態 |

### 4.5 使用するバックエンド

`SummarizerAgent` と `SummarizerModel` を流用（独自設定なし）。デフォルトは codex。

## 5. Summarizer（2層構造）

### 5.1 Finding Summarizer (`internal/summarizer/`)

複数レビュアの並列レビュー結果をLLMに渡し、重複する finding をクラスタリング・要約する。

- 入力: `[]domain.AggregatedFinding` → JSON
- 出力: `findings` (アクション要) と `info` (情報のみ) に分類
- 使用バックエンド: `SummarizerAgent` (デフォルト: codex)

### 5.2 Feedback Summarizer (`internal/feedback/`)

`gh pr view` 等で PR のコメント・リプライを取得し、LLM に渡して過去のコード指摘を構造化抽出する。

- 出力形式: `STATUS: "finding description" -- reason (by @author)`
- STATUS: DISMISSED / FIXED / ACKNOWLEDGED / INTENTIONAL
- 抽出結果は FP Filter のプロンプトに注入される
- 使用バックエンド: `PRFeedbackAgent` (未設定時は `SummarizerAgent` にフォールバック)
- レビュアーと並行して実行される（goroutine + sync.WaitGroup）

## 6. 各コンポーネントのデフォルトバックエンド

| コンポーネント | 設定キー | デフォルト |
|---|---|---|
| レビュアー | `ReviewerAgents` | `["codex"]` |
| Finding Summarizer | `SummarizerAgent` | `"codex"` |
| FP Filter | `SummarizerAgent` (流用) | `"codex"` |
| Feedback Summarizer | `PRFeedbackAgent` → `SummarizerAgent` | `"codex"` |

## 7. レビュアーエージェントの外部制御可能性

### 7.1 モデル指定

| 設定対象 | CLI フラグ | 環境変数 | .acr.yaml |
|---|---|---|---|
| レビュアー | `--reviewer-model` | `ACR_REVIEWER_MODEL` | `reviewer_model` |
| Summarizer / FP Filter | `--summarizer-model` | `ACR_SUMMARIZER_MODEL` | `summarizer_model` |

**制限**: モデル指定はレビュアー全体で1つ。エージェントごとの個別モデル指定は不可。異種エージェント混成時に `--reviewer-model` を指定すると、全バックエンドに同じ文字列が渡される。

### 7.2 各バックエンドの数（分配比率）

`--reviewer-agent` (`-a`) はカンマ区切りで複数エージェントを指定可能。分配はラウンドロビン：

```go
func AgentForReviewer(agents []Agent, reviewerID int) Agent {
    return agents[(reviewerID-1)%len(agents)]
}
```

例: `--reviewer-agent codex,claude,gemini -r 9` → 各3台ずつ均等分配

重み付けの直接指定はできないが、同じ名前を繰り返すことで擬似的に制御可能：
```
--reviewer-agent codex,codex,claude -r 6
→ codex:4, claude:2
```

### 7.3 guidance（スコープ / レビュー指示）

| 設定方法 | 優先順位 |
|---|---|
| `--guidance "テキスト"` | 1 (最高) |
| `--guidance-file path` | 2 |
| `ACR_GUIDANCE` 環境変数 | 3 |
| `ACR_GUIDANCE_FILE` 環境変数 | 4 |
| `.acr.yaml: guidance_file` | 5 |

guidance はレビュープロンプト中の `{{guidance}}` プレースホルダに展開される：

```go
func RenderPrompt(template, guidance string) string {
    return strings.ReplaceAll(template, "{{guidance}}", "\nAdditional context:\n"+guidance)
}
```

**制限**: 全レビュアーに同一の guidance が渡される。エージェントごとに異なる guidance を渡すことは不可。

### 7.4 その他の制御パラメータ

| 項目 | CLI フラグ | 環境変数 | .acr.yaml | デフォルト |
|---|---|---|---|---|
| レビュアー数 | `-r` | `ACR_REVIEWERS` | `reviewers` | 5 |
| 同時実行数 | `-c` | `ACR_CONCURRENCY` | `concurrency` | = reviewers |
| タイムアウト (レビュアー) | `-t` | `ACR_TIMEOUT` | `timeout` | 10m |
| リトライ回数 | `-R` | `ACR_RETRIES` | `retries` | 1 |
| ref-file モード | `--ref-file` | — | — | auto (大きいdiffで自動) |
| FP フィルタ有効 | `--no-fp-filter` | `ACR_FP_FILTER` | `fp_filter.enabled` | true |
| FP 閾値 | `--fp-threshold` | `ACR_FP_THRESHOLD` | `fp_filter.threshold` | 75 |
| Summarizer タイムアウト | `--summarizer-timeout` | `ACR_SUMMARIZER_TIMEOUT` | `summarizer_timeout` | 5m |
| FP Filter タイムアウト | `--fp-filter-timeout` | `ACR_FP_FILTER_TIMEOUT` | `fp_filter_timeout` | 5m |
| 除外パターン | `--exclude-pattern` | — | `filters.exclude_patterns` | — |
| PR feedback 有効 | `--no-pr-feedback` | `ACR_PR_FEEDBACK` | `pr_feedback.enabled` | true |
| PR feedback エージェント | `--pr-feedback-agent` | `ACR_PR_FEEDBACK_AGENT` | `pr_feedback.agent` | = summarizer |

設定優先順位: CLI フラグ > 環境変数 > .acr.yaml > デフォルト値

### 7.5 できること・できないことの整理

**できる:**
- バックエンドの種類選択（codex / claude / gemini の任意組合せ）
- 全レビュアー共通のモデル指定
- ラウンドロビンによる分配比率の擬似制御（名前の繰り返し）
- 全レビュアー共通の guidance 注入
- 並列数・タイムアウト・リトライの制御

**できない:**
- エージェントごとに異なるモデルを指定
- エージェントごとに異なる guidance / プロンプトを渡す
- 分配比率の明示的な重み指定
- レビュープロンプト自体のカスタマイズ（ハードコード）
- レビュアーごとに異なるタイムアウトやリトライ設定

## 8. 設計思想

- **レビュアーは均質な「投票者」**: 個別チューニングより「N人の独立レビュアーのコンセンサス」を重視
- **LLM 依存**: FP Filter も Summarizer も独自の NLP/ML モデルは使わず、全面的に LLM のプロンプトエンジニアリングに依存
- **fail-open**: LLM 実行失敗時はフィルタをスキップして全 finding を通す安全設計
- **CLI ラッパー**: LLM API を直接呼ばず、各ベンダーの CLI をサブプロセスとして起動することで認証・モデル管理を各 CLI に委譲
