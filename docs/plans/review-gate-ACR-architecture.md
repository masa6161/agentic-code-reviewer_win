# 設計: ACR_WIN への cf_skills コアワークフロー移植

## 目的

`docs/cf_skills`（codex-review / copilot-review）から抽出したコアワークフローのうち、
**どれを ACR_WIN に移植するか・しないか** を決定し、実装フェーズの設計根拠を残す。

---

## 確定したアーキテクチャ方針

### ACR + スキル構成（Hybrid）

```
呼び出し元エージェント（Claude Code スキル / Codex / Copilot）
  ↓ acr --format json ... を実行
ACR_WIN（one-shot CLI）
  ↓ phase-typed 並列 reviewer を実行
  ↓ ok / blocking / advisory を含む構造化 JSON を返す
呼び出し元エージェント
  ↓ ok: false → 修正 → 再度 acr を呼ぶ（ループ）
  ↓ ok: true → 終了
```

**ACR の責務（変更後）:**
- phase-typed 並列レビュー実行（arch + diff 等）
- `ok` / `blocking` / `advisory` を含む構造化 JSON 出力
- diff 規模の自動判定（小/中/大）

**ACR の責務外（変更なし）:**
- 「修正 → 再レビュー」ループの制御 → 外部エージェント
- 実際のコード修正 → 外部エージェント
- クォータ管理 → 外部

---

## 判断マトリクス

| 要素 | 移植容易性 | 効果 | 優先度 | 実装ポイント |
|------|-----------|------|--------|------------|
| **ok/blocking 出力スキーマ** | ◎ 高 | ◎ 高 | **最優先 (Phase 1)** | `Finding` に `Severity`/`Prefix`/`Category` を追加。`GroupedFindings` に `Ok bool` を追加 |
| **notes + skipped_files フィールド** | ◎ 高 | ○ 中 | **Phase 1（ok と同時）** | `Finding` / `GroupedFindings` にフィールド追加のみ |
| **phase-typed 並列実行** | ○ 中 | ◎ 高 | **Phase 2** | `Runner.Config` に `PhaseConfig` 追加。`ReviewConfig` に `Phase string` 追加 |
| **規模判定（S/M/L）** | ○ 中 | ○ 中 | **Phase 2（phase と同時）** | `git diff --stat` を解析。`internal/git/` に追加 |

**評価軸の定義:**
- 移植容易性: ACR コードベースへの変更量（◎ = ドメイン型のみ、○ = 複数パッケージ）
- 効果: 呼び出し元エージェント環境に依存しないレビューゲートとしての機能性

---

## 移植しないもの（Non-Goals）

| 要素 | 理由 |
|------|------|
| 反復ループ（`--gate` モード） | ACR は one-shot。ループは外部責務 |
| cross-check phase | 後述「なぜ現時点で設計しないか」参照 |
| クォータチェック | 環境非依存のため不要（codex は別途検討可） |
| PowerShell ランタイムスクリプト移植 | ACR は Go CLI |
| arch 専用 reviewer を複数本起動 | 意味がない。1本の arch + 1本の diff が正しい構成 |

---

## cross-check を現時点で設計しない理由

**large 判定時の grouped diff は頻繁に発生する**ため、cross-check は最終的にワークフロー全体に取り込む必要がある。  
しかし**今この段階で具体的設計を行わない**理由は以下のとおり。

### 前提が未実装

cross-check は「diff をグループ分割して並列レビューした結果を横断チェックする」フェーズである。  
そのために必要な以下がまだ実装されていない。

1. **Phase 0**: `ReviewerSpec` による per-reviewer `TargetFiles` 指定（grouped diff の基盤）
2. **Phase 2**: 規模判定（large 判定）と grouped diff の自動構成

土台のない段階で cross-check を設計するとインターフェース仕様が確定せず、後で全面改修になる。

### 直列依存を持つ唯一のフェーズ

arch / diff は同一スナップショット上で完全並列実行できる。  
cross-check のみが「全 diff-group reviewer の完了後に起動する」**直列依存**を持つ。

```
arch || diff.g01 || diff.g02 || diff.g03   ← 並列（現行モデルで表現可能）
              ↓ 全完了後
         cross-check                        ← 直列（現行モデルでは表現不可）
```

この「前フェーズの出力を次フェーズの入力にする」pipeline を ACR 内部に正しく実装するには、  
Runner の実行モデル自体の拡張が必要であり、Phase 0〜2 の後でないと設計が確定しない。

### cross-check の入力形式が未確定

cross-check は各 diff-group reviewer の**構造化 JSON 出力**（`ok`, `issues`, `skipped_files` 等）を入力に取る。  
現時点では ACR の出力スキーマ拡張（Phase 1）が未実装であり、  
cross-check の入力インターフェースを今設計しても Phase 1 完了後に変わる可能性が高い。

### 設計スコープの規律

現在のギャップ（Phase 0〜2）を解消せずに cross-check を先行設計すると、  
「GroupedDiff がどう動くか不明なまま cross-check のインターフェースを仮定する」  
投機的設計になり、後の変更コストが増大する。

### cross-check の現時点における代替

外部スキルが以下を担う（ACR は one-shot のまま）。

```
acr --phase diff --target-files group1 --format json → result_g01
acr --phase diff --target-files group2 --format json → result_g02
# 全グループ完了後
acr --phase cross-check --cross-check-input result_g01,result_g02 --format json
```

この形であれば ACR 内部に直列 pipeline を持たずに cross-check を実現できる。  
ただし `--phase cross-check` と `--cross-check-input` は Phase 3 以降での実装対象。

---

## Summarizer の責務変質（重要）

cf_skills ワークフロー移植に伴い、現行 summarizer の責務は変質する。  
特に **cross-check が summarizer の代替として機能できる** 設計は魅力的であり、将来的に対応すべき。

### 現行 summarizer の役割

```
N reviewers（同一 diff・同一プロンプト）→ findings
→ Summarizer: dedup + semantic cluster（LLM）
→ FP Filter
→ Output
```

N 人の「均質な投票者」が同じ diff を見た結果をまとめるためのフェーズ。  
目的は **重複排除とグルーピング**。

### Phase-typed 移行後の問題

arch reviewer と diff reviewer は異なるスコープで異なる観点を持つ。  
この混在した出力に現行 summarizer をそのまま適用するのは適切ではない。

- arch findings と diff findings をまとめて dedup するのは意味的に誤り
- arch 観点の issue と diff 観点の issue は本来異なるカテゴリとして扱うべき

### cross-check との責務比較

| | 現行 summarizer | cross-check |
|--|----------------|-------------|
| 入力 | 同じ diff を見た N 人の findings | 異なる diff グループを見た reviewer の structured output |
| 目的 | dedup + semantic clustering | グループ間の横断整合チェック（interface 不整合・認可漏れ等） |
| large diff での価値 | 低（N 人が同じファイルを見ていないため dedup が機能しにくい） | 高（グループ間の不整合こそが large diff の主要リスク） |

### 目指すべき設計（Phase 3 候補）

```go
type SummaryMode int
const (
    SummaryModeAggregate  SummaryMode = iota // 現行: dedup + cluster（small/medium で有効）
    SummaryModeCrossCheck                    // 新規: cross-group consistency（large で有効）
    SummaryModeBoth                          // 両方実行
)
```

**規模に応じた自動切り替え案:**

| 規模 | デフォルト動作 |
|------|--------------|
| small / medium | `SummaryModeAggregate`（現行のまま） |
| large（grouped diff あり） | `SummaryModeCrossCheck`（cross-check が summarizer を代替） |

`--summary-mode aggregate\|cross-check\|both` で明示的に切り替え可能にする。

### 現時点での設計制約

- cross-check の入力は Phase 1 の出力スキーマ（`ok`/`issues`/`skipped_files`）に依存する
- summarizer の on/off 切り替え自体は Phase 1 完了後から検討可能
- cross-check による代替は Phase 2（grouped diff 実装）完了後から実装可能

---

---

## 変更対象ファイル（Phase 1）

### `internal/domain/finding.go`

```go
// 現状
type Finding struct {
    Text       string
    ReviewerID int
}

// 変更後
type Finding struct {
    Text       string
    ReviewerID int
    Severity   string // "blocking" | "advisory"
    Prefix     string // "[must]" | "[imo]" | "[nits]" | "[fyi]" | "[ask]"
    Category   string // "correctness" | "security" | "perf" | "maintainability" | "testing" | "style"
    Phase      string // "arch" | "diff" | "cross-check" (空文字列 = 未指定)
}

// GroupedFindings に追加
type GroupedFindings struct {
    Ok                 bool          // blocking が 0 件なら true
    Findings           []FindingGroup
    Info               []FindingGroup
    NotesForNextReview string        // 次 iteration への引き継ぎメモ
    SkippedFiles       []string      // 未レビューファイル一覧
}
```

### `internal/agent/prompts.go`

- arch phase 用デフォルト prompt を追加
  - 観点: 依存関係、責務分割、破壊的変更、セキュリティ設計
- diff phase 用デフォルト prompt は既存のまま

---

## 変更対象ファイル（Phase 2）

### `internal/runner/runner.go`

```go
type Config struct {
    // 既存フィールドはそのまま
    Phases []PhaseConfig // 追加
}

type PhaseConfig struct {
    Phase        string // "arch" | "diff"
    ReviewerCount int
    Prompt       string // 空文字列 = phase のデフォルト prompt を使用
}
```

### `internal/agent/agent.go`

```go
type ReviewConfig struct {
    // 既存フィールドはそのまま
    Phase string // 追加: "arch" | "diff" | ""
}
```

### `cmd/acr/main.go`

```
--phase arch,diff     // phase-typed reviewer の指定（案）
--format json         // ok フィールドを含む JSON 出力
```

### `internal/git/`（新規関数）

```go
func ClassifyDiffSize(baseRef string) (DiffSize, error)
// DiffSize: Small (≤3 files / ≤100 lines)
//           Medium (4-10 files / 100-500 lines)  
//           Large (>10 files / >500 lines)
```

---

## 受け入れ基準

- [ ] `acr ... --format json` が `ok` (bool) を含む JSON を返す
- [ ] blocking Finding が 1 件以上あるとき `ok: false`
- [ ] blocking Finding が 0 件のとき `ok: true`（advisory は含んでよい）
- [ ] `--phase arch,diff` で arch+diff を同時並列実行できる
- [ ] diff サイズ自動判定で reviewer 構成が変わる（`--auto-phase`）
- [ ] `notes_for_next_review` / `skipped_files` が出力に含まれる

---

## Phase 2 の前提となる内部フレームワーク変更（Phase 0）

Phase-typed 並列実行を実現するには、現行の「均質 N 投票者」設計を「役割付き reviewer スペック」設計に移行する必要がある。
以下の 3 つのギャップが根本原因として確認された（根拠: `agentic-code-reviewer-analysis.md` §7）。

### Gap 1: レビュワーごとに異なるモデルを指定できない

**根本原因**: `model` は `CodexAgent.model` に構築時に焼き付けられており、`ReviewConfig` にモデルフィールドがない。

```go
// 現状 (internal/agent/codex.go)
type CodexAgent struct {
    model string  // 全レビュワー共通
}

// ReviewConfig にモデル指定なし (internal/agent/config.go)
type ReviewConfig struct {
    BaseRef, Timeout, WorkDir, Verbose, Guidance, ...  // Model なし
}
```

### Gap 2: レビュワーごとに異なる guidance/役割を注入できない

**根本原因**: `Runner.Config.Guidance` が単一文字列で、`runReviewer` が reviewer ID を無視して全員に同じ値を渡す。

```go
// 現状 (internal/runner/runner.go)
reviewConfig := &agent.ReviewConfig{
    Guidance: r.config.Guidance,  // 全員同じ
}
```

### Gap 3: レビュワーごとに異なる diff/対象ファイルを渡せない

**根本原因**: `Runner.Config.Diff` が単一文字列。grouped diff（ファイルセット A はレビュワー1、セット B はレビュワー2）を表現する構造がない。

```go
// 現状 (internal/runner/runner.go)
reviewConfig := &agent.ReviewConfig{
    Diff:            r.config.Diff,   // 全員同じ
    DiffPrecomputed: r.config.DiffPrecomputed,
}
```

### 解決策: ReviewerSpec 型の導入

現行の「`[]agent.Agent` → round-robin」をやめ、「`[]ReviewerSpec` → spec 直参照」に移行する。

```go
// 新設 (internal/runner/runner.go または internal/agent/spec.go)
type ReviewerSpec struct {
    Agent       agent.Agent // どのエージェントを使うか
    Model       string      // Gap 1: per-reviewer モデル (空 = Agent デフォルト)
    Phase       string      // Gap 2: "arch" | "diff" | "" (デフォルト: diff)
    Guidance    string      // Gap 2: phase 固有の役割指示
    Diff        string      // Gap 3: reviewer 固有の diff 本文 (空 = グローバル diff)
    TargetFiles []string    // Gap 3: grouped diff 用対象ファイル
}
```

**Runner の変更点:**

```go
// 変更前
type Runner struct {
    agents []agent.Agent   // round-robin
}

// 変更後
type Runner struct {
    specs  []ReviewerSpec  // reviewer ごとの仕様 (len == Config.Reviewers)
    agents []agent.Agent   // 後方互換 or 削除
}

// runReviewer: spec 直参照に変更
func (r *Runner) runReviewer(ctx context.Context, reviewerID int) domain.ReviewerResult {
    spec := r.specs[reviewerID-1]
    reviewConfig := &agent.ReviewConfig{
        Model:    spec.Model,    // Gap 1
        Guidance: spec.Guidance, // Gap 2
        Phase:    spec.Phase,    // Gap 2
        Diff:     orDefault(spec.Diff, r.config.Diff), // Gap 3
        TargetFiles: spec.TargetFiles,                 // Gap 3
        ...
    }
    return spec.Agent.ExecuteReview(timeoutCtx, reviewConfig)
}
```

**ReviewConfig の追加フィールド:**

```go
// internal/agent/config.go に追加
type ReviewConfig struct {
    // 既存フィールドはそのまま
    Model       string   // Gap 1: per-reviewer モデルオーバーライド
    Phase       string   // Gap 2: "arch" | "diff" | "cross-check"
    TargetFiles []string // Gap 3: grouped diff 用対象ファイル
}
```

**各 Agent の ExecuteReview での参照:**

```go
// codex.go: Agent デフォルト → ReviewConfig.Model の順で優先
model := config.Model
if model == "" {
    model = c.model
}
```

### フレームワーク変更の依存関係

```
[Phase 0] ReviewConfig に Model/Phase/TargetFiles 追加
       ↓
[Phase 0] ReviewerSpec 型の定義
       ↓
[Phase 0] Runner を specs ベースに変更 + Agent.ExecuteReview で config.Model を参照
       ↓
[Phase 2] phase-typed 並列実行 (arch+diff)
[Phase 2] 規模判定 → ReviewerSpec の自動生成
       ↓
[CLI]  --phase arch,diff / --format json フラグ
```

---

## 未実行タスク

### Phase 0: フレームワーク基盤（Phase 2 の前提）

- [ ] `internal/agent/config.go` に `Model`/`Phase`/`TargetFiles` フィールドを追加
- [ ] `internal/runner/runner.go` に `ReviewerSpec` 型を定義
- [ ] `Runner` を `[]agent.Agent` round-robin から `[]ReviewerSpec` 直参照に変更
- [ ] 各 Agent (`codex.go`/`claude.go`/`gemini.go`) で `config.Model` を参照するよう変更
- [ ] 後方互換テスト: 既存の `go test ./internal/agent ./internal/runner` が通ること

### Phase 1: 出力スキーマ拡張

- [ ] `internal/domain/finding.go` の拡張（`Severity`/`Prefix`/`Category`/`Phase`）
- [ ] `internal/agent/prompts.go` への arch phase プロンプト追加
- [ ] parser 側（codex/claude/gemini）の blocking/advisory 構文解析実装
- [ ] `GroupedFindings` に `Ok bool`/`NotesForNextReview`/`SkippedFiles` を追加

### Phase 2: phase-typed 並列実行 + 規模判定

- [ ] `Runner.Config` への `PhaseConfig`（または `ReviewerSpec` 自動生成ロジック）追加
- [ ] `internal/git/` への diff 規模判定関数追加
- [ ] `cmd/acr/main.go` への `--phase` / `--format json` フラグ追加
- [ ] テスト: arch+diff 並列実行、grouped diff、blocking/advisory 判定

---

## 次スレッドでやるべきこと

1. **Phase 0 から着手**: `ReviewConfig` への 3 フィールド追加（最小差分）
2. `Runner` の `[]agent.Agent` → `[]ReviewerSpec` 移行（後方互換を保ちながら）
3. `go test ./internal/agent ./internal/runner` でリグレッション確認
4. Phase 0 完了後、Phase 1（出力スキーマ拡張）へ進む

---

*作成日: 2026-04-12*  
*更新日: 2026-04-12（フレームワーク Gap 分析追記）*  
*セッション: deep-interview (cf_skills 調査 → 設計決定) + フレームワーク Gap 分析*  
*スペックファイル: `.omc/specs/deep-interview-acr-cf-port.md`（詳細版）*
