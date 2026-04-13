# 調査: docs/cf_skills の2スキル共通ワークフローと ACR_WIN への移植方針

## 目的

`docs/cf_skills/codex-review` と `docs/cf_skills/copilot-review` の両スキルを調査し、
共通するコアワークフローを抽出したうえで、ACR_WIN への移植方針を立てる。

---

## 調査結果

### 両スキルの共通目的

**反復レビューゲート (Iterative Review Gate)**

どちらのスキルも本質的に同じ目的を持つ。

- LLM subagent にコードレビューを委譲する
- `ok: true` になるまで「レビュー → 修正 → 再レビュー」を反復する
- 全変更が承認されるか、上限 iteration に達するまで止まらない

### 共通コアワークフロー

```text
┌─────────────────────────────────────────────────────┐
│  ITERATION START                                    │
│                                                     │
│  1. 規模判定                                        │
│     git diff --stat / --name-status                 │
│     → small (≤3files/≤100lines)                     │
│     → medium (4-10files / 100-500lines)             │
│     → large (>10files / >500lines)                  │
│                                                     │
│  2. Diff Artifact 取得 (クォータチェック含む)         │
│                                                     │
│  3. Phase 並列実行 (同一 run_id / 同一 snapshot)     │
│     small:  [diff]                                  │
│     medium: [arch] || [diff]                        │
│     large:  [arch] || [diff.g01 ... diff.gNN]       │
│               → [cross-check] (全 diff 完了後)      │
│                                                     │
│  4. 全 Phase 結果統合                               │
│     ok: true  → advisory 確認 → 終了                │
│     ok: false → 次ステップへ                         │
│                                                     │
│  5. 修正 (呼び出し元エージェントが実施)              │
│     テスト/リンタ実行                                │
│     新 run_id で次 iteration へ                      │
└─────────────────────────────────────────────────────┘

停止条件:
  - ok: true
  - max_iters 到達 (既定 3, 上限 5)
  - テスト 2 回連続失敗
  - 指摘が振動 (A→B→A の逆向き指摘) → ユーザーに確認
```

### 共通出力スキーマ

両スキルで完全に同一のスキーマを使用する。

```json
{
  "ok": true,
  "phase": "arch|diff|cross-check",
  "summary": "レビューの要約",
  "issues": [
    {
      "severity": "blocking|advisory",
      "prefix": "[must]|[imo]|[nits]|[fyi]|[ask]",
      "category": "correctness|security|perf|maintainability|testing|style",
      "file": "src/foo.go",
      "lines": "42-45",
      "problem": "問題の説明",
      "recommendation": "修正案"
    }
  ],
  "notes_for_next_review": "...",
  "skipped_files": []
}
```

**判定ルール:**
- `ok`: blocking issue が 0 件なら true、1 件以上なら false
- blocking = 修正必須 (`[must]` prefix)
- advisory = 推奨/警告 (`[imo]` / `[nits]` / `[fyi]` / `[ask]`)
- advisory は `ok: true` でも出力可。ユーザー確認後に修正判断

### スキル間の主な差異

| 観点 | codex-review | copilot-review |
|------|-------------|----------------|
| レビューエンジン | Codex CLI (PowerShell gate script) | VS Code Copilot Chat worker agent |
| クォータチェック | あり (quota-check.ps1) | 不要 (runSubagent はプレミアム不消費) |
| Phase ごとのモデル | 単一 (gpt-5.4 固定) | arch/cross-check=Opus4.6, diff=gpt-5.4 |
| Diff 取得 | gate script 内部で git diff | copilot-review-diff.ps1 で artifact 生成 |
| Hash 検証 | gate.ps1 が core.ps1 の SHA256 を検証 | なし |
| 実行環境 | 任意のターミナル | VS Code Copilot Chat のみ |
| untracked files | DiffRange=HEAD で自動収集 | 代替 index で `git add -N` |

---

## ACR_WIN の現状と不足要素

### ACR_WIN が既に持つ機能

| cf_skills の概念 | ACR_WIN の対応箇所 |
|----------------|------------------|
| 並列 reviewer 実行 | `internal/runner/runner.go` |
| 複数 LLM バックエンド | `internal/agent/` (codex/claude/gemini) |
| Semantic clustering | `internal/summarizer/` |
| False positive フィルタ | `internal/fpfilter/` |
| 構造化 Finding | `internal/domain/finding.go` |

### ACR_WIN に不足している要素

1. **反復レビューループ** — 現在は one-shot。`ok: false` → fix → re-review のループがない
2. **ok/blocking/advisory 判定ゲート** — severity はあるが `ok` というバイナリゲートがない
3. **規模別 Phase 戦略** — small/medium/large で実行する phase を切り替える仕組みがない
4. **arch phase** — アーキテクチャ整合性レビューが独立 phase として存在しない
5. **cross-check phase** — グループ間の横断整合チェック (summarizer とは別概念)
6. **`notes_for_next_review` 伝播** — iteration 間のコンテキスト引き継ぎ
7. **クォータ/レート制限チェック** — 各 iteration 前の利用枠確認

---

## 移植方針

### 移植すべきコアワークフロー

ACR_WIN への移植で最も価値が高いのは次の3点。

#### (1) 反復レビューループ (`--gate` モード)

```
acr --gate --base HEAD~1 --max-iters 3
```

- 初回レビューで `blocking` findings が出たら → 修正を促す
- ユーザーが修正後に再実行 → 同じ diff range で再レビュー
- `ok: true` (blocking 0件) になるまで繰り返す

#### (2) ok/blocking/advisory 構造化出力

既存の `Finding.Severity` に対応:

```go
// 既存
type Finding struct {
    Severity string  // "high"/"medium"/"low"
    ...
}

// 移植後の拡張案
type Finding struct {
    Severity  string  // "blocking" | "advisory"
    Prefix    string  // "[must]" | "[imo]" | "[nits]" | "[fyi]" | "[ask]"
    Category  string  // "correctness" | "security" | "perf" | ...
    ...
}

type ReviewResult struct {
    Ok     bool
    Issues []Finding
    NotesForNextReview string
    SkippedFiles []string
}
```

#### (3) 規模別戦略

```go
type ReviewScale int
const (
    ScaleSmall  ReviewScale = iota  // ≤3 files, ≤100 lines
    ScaleMedium                      // 4-10 files, 100-500 lines
    ScaleLarge                       // >10 files, >500 lines
)
```

- Small: diff reviewer × 1
- Medium: arch reviewer + diff reviewer (並列)
- Large: arch reviewer + grouped diff reviewers + cross-check reviewer

### 移植の難易度・優先度

| 要素 | 難易度 | 優先度 | 備考 |
|------|--------|--------|------|
| ok/blocking 判定 | 低 | 高 | parser と domain の変更のみ |
| 反復ループ | 中 | 高 | cmd/acr/main.go に loop を追加 |
| 規模判定 | 低 | 中 | git diff --stat を解析するだけ |
| arch phase | 中 | 中 | reviewer prompt を arch 用に変更 |
| cross-check phase | 高 | 低 | 複数 reviewer 結果を入力に取る new agent |
| notes_for_next_review | 低 | 中 | Finding/Result に field 追加 |
| クォータチェック | 中 | 低 | codex のみ必要、claude/gemini は不要 |

---

## 未実行タスク

- [ ] `internal/domain/finding.go` の拡張 (blocking/advisory, prefix, category)
- [ ] `internal/runner/runner.go` へのループ実装
- [ ] 規模判定ロジックの追加 (`internal/git/` or `cmd/acr/`)
- [ ] arch phase 用プロンプトテンプレートの作成 (`internal/agent/prompts.go`)
- [ ] `--gate` フラグの追加 (`cmd/acr/main.go`)
- [ ] cross-check phase の設計 (大規模変更向け)

---

## 次スレッドでやるべきこと

1. `internal/domain/finding.go` を読み、blocking/advisory 拡張の最小差分を確認する
2. `cmd/acr/main.go` の現状のループ構造を確認し、`--gate` フラグの追加位置を特定する
3. `internal/agent/prompts.go` を確認し、arch phase 用プロンプトを追加する
4. まず (1) ok/blocking 判定、(2) 反復ループ の2点に絞って実装を開始する

---

---

## 重要な設計判断ポイント（移植前に決定必要）

### 「修正担当者」問題

両スキルの反復ループが成立するのは、ループの外側に「呼び出し元コーディングエージェント」が存在するためである。
スキルはレビュー指摘を出すだけで、実際の修正はそのエージェントが担う。

**ACR_WIN はコードを修正しない CLI ツール**である。したがって反復ループを移植する場合、次の2つのアーキテクチャ方向のどちらかを選択する必要がある。

#### 方向 A: ACR を「コンポーネント」として使う（推奨）

ACR の出力スキーマを強化し、外部のオーケストレーター (Claude Code / Copilot Chat 等) がループを駆動する。

```
外部エージェント
  → acr --format json ... (1回のレビュー実行)
  → ok: false なら修正
  → 再度 acr を呼ぶ
  → ok: true で終了
```

- ACR 自体は one-shot のまま
- 移植スコープ: 出力スキーマの拡張 + 規模判定 + arch/diff/cross-check phase 分離
- ACR の CLI 性質を保つ。軽量かつ既存設計と整合

#### 方向 B: ACR 自体がループを駆動する（大規模変更）

ACR がコーディングエージェント呼び出しを内部化し、fix → re-review を自律実行する。

- 移植スコープ: 全ループ実装 + エージェント連携 (claude --tools 等)
- ACR の責務が大幅に増加し、既存設計からの逸脱が大きい
- 現時点では採用しない

### 移植スコープの再整理

方向 A を前提とすると、**どちらの方向でも価値のある**共通移植要素は次の通り。

| 要素 | 理由 |
|------|------|
| `ok` / `blocking` / `advisory` 出力スキーマ | 外部ループが `ok: false` を検知するために必須 |
| `notes_for_next_review` フィールド | 次 iteration への文脈引き継ぎ |
| `skipped_files` フィールド | 未レビュー差分の明示 |
| 規模判定 (small/medium/large) | 実行 phase 数の最適化 |
| arch phase プロンプト | アーキテクチャ整合性の独立レビュー |

ループ本体・cross-check phase は方向 A では外部側の責務。

### Summarizer と Cross-check の違い（補足）

- **ACR の summarizer**: 同一 diff に対する複数 reviewer の結果を dedup/cluster する
- **Cross-check**: 大規模 diff をグループ分割した際の「グループ間の整合性」を確認する

目的が異なる。ACR の summarizer は cross-check の代替ではない。

### ACR_WIN の既存強みとの対応

`runner/agent` の親子構造は cf_skills の「親オーケストレーター / 子 subagent」と対応しており、
移植の土台として適している。

| cf_skills | ACR_WIN |
|-----------|---------|
| 親エージェント (phase 管理) | `internal/runner/runner.go` |
| 子 subagent (1 phase 実行) | `internal/agent/*.go` |
| 結果統合 | `internal/summarizer/` |

---

*作成日: 2026-04-12*
*セッション: cf_skills 調査 (ultrawork)*
