# バックログ トリアージ 2026-04-23

対象: `backlog/2026-04-22_03.md` の持ち越しタスク 16件

## CLOSED — 対応済み (5件)

PR #4〜#6 の開発サイクル中に解決済みだが、バックログの持ち越しリストに残存していたもの。
各セッションでの持ち越しリスト転記時に現状確認が行われなかったことが原因。

| # | タスク | 初出 | 解決された PR / 時期 | 根拠 |
|---|--------|------|---------------------|------|
| F#5 | `buildGroupedDiffSpecs` empty group fallback | 2026-04-21_01 | PR #4 (grouped-diff-autophase) | 3層防御実装済み: pre-build cap (`effectiveGroups<2`), `buildGroupedDiffSpecs` error return (zero sections/groups), post-build `diffGroupCount<2` fallback。テスト: `TestBuildGroupedDiffSpecs_EmptyGroupsSkipped_IDsContiguous`, `TestBuildGroupedDiffSpecs_FallbackOnError`, `TestAutoPhase_Large_FallbackOnError` |
| F#1/CG#1 | validate 緩和・契約変更 | 2026-04-18_02 | PR #5 + PR #6 | 2-tier validation (`ValidateAll` parse-time + `ValidateRuntime` merged-time) + deferred cross-check resolution (`review.go:147-158`)。`canResolveCrossCheckModelForAgent` は `sizes["large"]` のみ検査し runtime 対称性を維持 |
| F#2 | grouped cross-check fail-fast 精緻化 | 2026-04-21_03 | PR #6 | 3-tier: validate-time structural → lazy runtime gate (`review.go:457-529`) → graceful degradation (`CrossCheck` never returns error)。`IsDegraded()` で verdict 反映 |
| F#4/CG#4 | sizes.small/medium 能動 reject | 2026-04-21_03 | PR #5 (new-agent-with-options) | `validateModelsSizes()` (`config.go:705-718`) が unknown size key を reject。テスト: `TestConfig_ModelsSizes_UnknownSizeKeyRejected` 等 |
| DiffPrecomputed | 調査 (Round-14 F#3) | 2026-04-13_01 | 調査完了 (問題なし) | 活用中の最適化。`ReviewConfig.DiffPrecomputed` が reviewer 間の redundant `git diff` を回避。Codex は意図的に無視 (built-in diff via `--base`) |

---

## 即時 PR 候補 (5本)

### PR A: Docstring・小修正

**見積: ~30min** | リスク: なし (行動変更なし)

| 修正 | ファイル | 内容 | 規模 |
|------|----------|------|------|
| C-2 docstring 強化 | `internal/summarizer/summarizer.go:293-296` | Rule B docstring に "LLM-claimed blocking without source citation is downgraded" を明記。現状 "LLM-fabricated" という表現で部分的に存在するが、backlog が求める明示性が不足 | 1行追加 |
| Beta notice 整理 | `cmd/acr/pr_submit.go:395,493` | `betaNoticeOnce` に Issue #3 への TODO コメント追加、またはユーザー判断で削除 | 数行 |

### PR B: `small_diff_reviewers` ノブ導入 (Issue #8)

**見積: ~2h** | リスク: 低 (既存 `medium_diff_reviewers` パターンの複製)

**設計判断 (確定)**: small diff で top-level `reviewers` (default 5) にフォールバックするのは設計意図に反する。移植元の設計では「規模が小さい → reviewer も少数で OK」が原則。レガシー動作（多数 reviewer 並列）が必要なら `--no-auto-phase` で明示的に呼び出す導線が既に存在する。

実装方針 (`medium_diff_reviewers` と同パターン):

| 層 | 変更箇所 | 内容 |
|----|----------|------|
| YAML | `internal/config/config.go` Config struct | `SmallDiffReviewers *int` フィールド追加 |
| Defaults | `config.go` defaults | default = 1 (小規模 diff は 1 reviewer で十分) |
| Validation | `config.go` `ValidateAll()` | `small_diff_reviewers >= 1` チェック追加 |
| Env | `config.go` env loading | `ACR_SMALL_DIFF_REVIEWERS` 追加 |
| CLI flag | `cmd/acr/main.go` | `--small-diff-reviewers` flag 追加 |
| Resolution | `config.go` resolve cascade | YAML → env → CLI の3層 precedence |
| ResolvedConfig | `config.go` | `SmallDiffReviewers int` フィールド追加 |
| Auto-phase | `cmd/acr/review.go` `resolveAutoPhase` small path | `resolved.Reviewers` → `resolved.SmallDiffReviewers` に変更 |
| config show | `cmd/acr/config_cmd.go` | 表示に追加 (PR C と統合可) |
| テスト | `config_test.go`, `review_test.go` | validation + auto-phase small path テスト |

### PR C: `config show` 拡張

**見積: ~2-3h** | リスク: なし (表示ロジック追加のみ)

対象: `cmd/acr/config_cmd.go:30-68`

現在表示されている14フィールド: `reviewers`, `concurrency`, `base`, `timeout`, `retries`, `fetch`, `reviewer_agents`, `summarizer_agent`, `summarizer_timeout`, `fp_filter_timeout`, `fp_filter.enabled`, `fp_filter.threshold`, `pr_feedback.enabled`, `pr_feedback.agent`

追加すべきフィールド:
- `auto_phase` (ResolvedConfig.AutoPhase, default true)
- `diff_groups` (ResolvedConfig.DiffGroups, default 4)
- `medium_diff_reviewers` (ResolvedConfig.MediumDiffReviewers, default 2)
- `cross_check.enabled` (ResolvedConfig.CrossCheckEnabled, default true)
- `cross_check.agent` (ResolvedConfig.CrossCheckAgent)
- `cross_check.model` (ResolvedConfig.CrossCheckModel)
- `cross_check_timeout` (ResolvedConfig.CrossCheckTimeout, default 5m)
- `models` tree (Defaults/Sizes/Agents 各層)
- `strict` (ResolvedConfig.Strict)
- `arch_reviewer_agent` (ResolvedConfig.ArchReviewerAgent)
- `diff_reviewer_agents` (ResolvedConfig.DiffReviewerAgents)
- `guidance_file` (ResolvedConfig.GuidanceFile)

`config init` テンプレート (`config_cmd.go:88-145`) の拡充も含める。

### PR D: 起動時 CLI 可用性チェック (Issue #7)

**見積: ~2-3h** | リスク: 低 (早期 fail-fast、既存 IsAvailable 基盤を利用)

現状: `IsAvailable()` (`exec.LookPath`) は各 agent に実装済み。呼び出しは review 実行の深部で遅延的に行われる。

実装方針:
1. `executeReview` 冒頭 (reviewer/summarizer agent 構築直後) で一括チェック
2. cross-check agent は `resolved.AutoPhase && resolved.CrossCheckEnabled` 条件付き
3. `gh` CLI は PR 関連フラグ (`--submit` 等) 設定時にチェック
4. 失敗時: 不足 CLI 名 + インストール指示付きエラーメッセージ → `domain.ExitError`
5. `--verbose` 時: 利用可能 CLI の一覧ログ出力

### PR E: N/M 表記ロール別分離

**見積: ~3-4h** | リスク: 中 (domain 型 + 全レポート箇所の変更)

**方針**: 現行の `(N/M reviewers)` はフェーズ (arch/diff) を混在集計しており、disjoint group 設計下で誤解を招く。ロール別に分離して表示する。

**目標出力例**:
- grouped review: `(arch: 1/1, diff: 2/4 reviewers)` — arch は 1 reviewer 中 1 人が指摘、diff は 4 reviewer 中 2 人が指摘
- flat review (phase なし): `(3/5 reviewers)` — 従来通り
- LGTM banner: `(arch: 1/1, diff: 4/4 reviewers)`

**データフロー分析**:

```
Finding.Phase ("arch"|"diff")     ← runner.go:376 で設定済み
Finding.GroupKey ("arch"|"g01"…)  ← runner.go で設定済み
        ↓
AggregatedFinding.GroupKey        ← domain.AggregateFindings() で伝播済み
        ↓
FindingGroup.Sources []int        ← summarizer が AggregatedFinding index を返す
FindingGroup.ReviewerCount int    ← 現状フェーズ混在
        ↓ ★ここから変更が必要
ReviewStats.TotalReviewers int    ← 現状フェーズ混在
```

**実装箇所**:

| 変更 | ファイル | 内容 |
|------|----------|------|
| ReviewStats 拡張 | `internal/domain/result.go` | `ArchReviewers int`, `DiffReviewers int` フィールド追加 |
| Stats 集計 | `internal/runner/runner.go:438` | spec.Phase に基づき per-phase カウント |
| FindingGroup 拡張 | `internal/domain/finding.go` | `ArchReviewerCount int`, `DiffReviewerCount int` 追加 (or Sources の phase 別集計ヘルパー) |
| Source → Phase マッピング | `internal/summarizer/summarizer.go` | `AggregatedFinding.GroupKey` から arch/diff を判別し FindingGroup に反映 |
| Terminal report | `internal/runner/report.go:105-107` | ロール別フォーマット |
| PR markdown | `internal/runner/report.go:313-314` | ロール別フォーマット |
| LGTM banner | `internal/runner/report.go:250-253` | ロール別フォーマット |
| Raw findings | `internal/runner/report.go:524` | ロール別フォーマット |
| Selector | `internal/terminal/selector.go:125-131` | ロール別フォーマット (現状 denominator なしの不整合も同時修正) |
| テスト | `report_test.go`, `finding_test.go` | 新フォーマットのアサーション |

**フォーマット関数**: `formatReviewerCount(finding, stats)` を1つ作り、全5箇所から呼び出す。flat review (Phase が空) の場合は従来の `(N/M reviewers)` にフォールバック。

**備考**: project memory `project_finding_count_redesign.md` にある根本的問題 (LLM clustering 非決定性、disjoint group ミスマッチ) の完全解決ではない。ロール別分離は「arch 1 reviewer + diff 4 reviewer のうち何人が同意したか」を正確に伝える改善であり、clustering 品質の問題とは直交する。

---

## PARKED — 現時点で着手不要 (2件)

| タスク | 初出 | 理由 |
|--------|------|------|
| C-3 (1000 rune 閾値再評価) | 2026-04-22_02 | agent 数増加時の実運用データが必要。現状のデフォルト値で問題報告なし |
| Gemini thinking control API | 2026-04-18_01 | Gemini CLI が thinking effort API を未提供。`geminiEffortArgs()` の no-op stub が正しい状態。API 提供時に実装 |

---

## 設計判断が必要 (1テーマ)

### Prompt Engineering Bundle (3件 → 別 PR 1-2本)

以下3件は因果関係で連鎖: reviewer が severity を出さない → summarizer が推測 → convergence 信号が弱い → coverage gap 検出不可。

| 件名 | 現状 | 見積 |
|------|------|------|
| Reviewer blocking/advisory 判定基準 | diff-phase プロンプト (`prompts.go`) に severity 指針なし。arch prompt のみ `[must]`/`[imo]` を指示。severity は summarizer の post-hoc 推定に全面依存 | ~3h (プロンプト変更 + パーサー調整の可能性) |
| Cross-group convergence 信頼性 | cross-check prompt は inter-group 矛盾/gap 検出のみ。convergence (合意度) シグナルなし | ~2h (プロンプト追加) |
| CG#2/CG#3 reviewer coverage gap | intra-group reviewer 間 divergence 検出/報告なし。`ReviewerCount`/`Sources` データは存在するが未活用。FP filter の `agreementBonus` (filter.go:97-117) はフィルタリング用で報告用ではない | ~3h (報告ロジック追加) |

**合計見積: 設計議論 2h + 実装 4-6h (PR 1-2本)**

事前に設計方針の合意が必要:
- diff-phase プロンプトに severity 指針を追加するか、summarizer 推定を維持するか
- coverage gap を report 表示するか、verbose のみにするか
- convergence シグナルの定量化方法 (agreement ratio threshold 等)

---

## 推奨着手順

```
優先度  タスク                         見積    前提条件
───────────────────────────────────────────────────────
1       PR A (docstring・小修正)        ~30min  なし (即着手可)
2       PR B (small_diff_reviewers)    ~2h     なし (Issue #8 設計確定済み)
3       PR C (config show 拡張)        ~2-3h   PR B と統合可 (config show にフィールド追加)
4       PR D (CLI check, Issue #7)     ~2-3h   なし
5       PR E (N/M ロール別表記)         ~3-4h   なし (domain 型変更あり)
6       Prompt Engineering Bundle      ~8h     設計議論
```

PR A は即着手可。PR B と PR C は config 領域が重なるため同一 PR に統合してもよい。
PR E は domain 型変更を伴うため独立 PR が望ましい。
合計見積 (Prompt Engineering Bundle 除く): ~10-12h
