# ACR Role-Prompts Benchmark: 定性評価

**実施日**: 2026-04-29/30
**評価ルーブリック**: 有用 / 冗長 / 的外れ (計画 Post-TDD セクション準拠)

---

## テーマ別 Finding 分類

全12 trial (baseline×6 + treatment×6) の summarizer findings を意味的に集約すると、以下の **6 テーマ** に収束する。

### Theme 1: Small/flat phase で cross-file カバレッジが消失

- **内容**: `AutoPhaseDiffPrompt` が「arch reviewer が full diff を担当するので cross-file は Skip」と指示するが、small/flat phase では arch reviewer が存在しない。`RolePrompts=true` + small phase で cross-file findings が構造的にゼロになる。
- **出現**: Baseline 6/6, Treatment 6/6 (**全 trial で検出**)
- **Severity**: blocking (全 trial)
- **評価**: **有用** — 実際の設計バグ。`resolvePrompts` が arch reviewer の有無を考慮せず Phase 文字列のみでプロンプトを選択するため、auto-phase small + RolePrompts=true で coverage gap が生じる。ただし RolePrompts=false (デフォルト) では到達しない。
- **対応**: `resolvePrompts` に arch presence ガードを追加 or small phase で RolePrompts を無効化

### Theme 2: CLI flag precedence バグ (`--no-role-prompts=false`)

- **内容**: `RolePrompts: rolePrompts && !noRolePrompts` + `RolePromptsSet = Changed("role-prompts") || Changed("no-role-prompts")` の組合せで、`--no-role-prompts=false` が `RolePromptsSet=true / RolePrompts=false` を流し込み、`ACR_ROLE_PROMPTS=true` や yaml 設定を黙って上書きする。
- **出現**: Baseline 6/6, Treatment 6/6 (**全 trial で検出**)
- **Severity**: blocking (全 trial)
- **評価**: **有用** — 実バグ。`--no-role-prompts=false` は「否定フラグを明示的に false にする」edge case だが、フラグペアの合成ロジックが precedence 契約を破る。AutoPhase フラグと同パターンで `noRolePromptsSet` を分離すべき。
- **対応**: フラグペアを tri-state に分離

### Theme 3: Ref-file プロンプトの「read this file」指示欠落

- **内容**: `AutoPhaseDiffRefFilePrompt` / `AutoPhaseArchRefFilePrompt` が「Review the code changes in %s」のみで、既存 ref-file プロンプトにある「Read the diff content from this file」相当の指示を欠く。large diff + ref-file mode で agent が diff を読まずにレビューするリスク。
- **出現**: Baseline 5/6, Treatment 6/6
- **Severity**: advisory→blocking (cross-check で escalation)
- **評価**: **有用** — 実際の regression。既存の `DefaultClaudeRefFilePrompt` 等には明示的な読込指示があり、新プロンプトで欠落している。
- **対応**: ref-file プロンプトに明示的な file read 指示を追加

### Theme 4: Codex agent パスの RolePrompts バイパス

- **内容**: `resolvePrompts` が `executeDiffBasedReview` からのみ呼ばれるため、Codex が別パスを通る場合に RolePrompts が no-op になる。
- **出現**: Baseline 5/6, Treatment 3/6
- **Severity**: blocking (baseline) / blocking+disputed (treatment)
- **評価**: **的外れ** — Codex の `ExecuteReview` は内部で `executeDiffBasedReview` を呼び出している (`codex.go:60`)。Codex も Claude/Gemini と同じパスを通るため、RolePrompts は正しく適用される。Reviewer が diff のみから判断し、codex.go の実装を確認せずに推測した誤検知。
- **Treatment での改善**: Treatment の trial 1 で CC1/CC5 が「disputed」として検出。Role-specific prompt により reviewer がより慎重に判断し、誤検知の dispute が増えた可能性がある。

### Theme 5: 未知 Phase の default fallback

- **内容**: `resolvePrompts` の `default:` 分岐が未知 Phase を `AutoPhaseDiffPrompt` に黙ってフォールバックさせる。
- **出現**: Baseline 3/6, Treatment 0/6 (treatment では独立 finding として出現せず)
- **Severity**: advisory
- **評価**: **冗長** — Phase 値は内部で制御されており（`parsePhases` が生成）、ユーザー入力は `--phase` バリデーションで small/medium/large に限定済み。正しい指摘だが実害は極めて低い。

### Theme 6: arch/diff プロンプト出力形式の混在（パーサ互換性）

- **内容**: arch プロンプトが `[must]/[imo]` 行頭、diff プロンプトが `file:line:` 形式と出力形式が混在。パーサの互換性が未検証。
- **出現**: Baseline 1/6 (trial 6), Treatment 0/6
- **Severity**: blocking (baseline trial 6)
- **評価**: **冗長** — 既存の `DefaultArchPrompt` も同様に `[must]/[imo]` 形式を使用しており、パーサは既にこの形式に対応済み。新プロンプトで新たに導入された問題ではない。

---

## 条件別定性サマリー

| テーマ | 評価 | Baseline 出現 | Treatment 出現 | 差異 |
|--------|------|--------------|---------------|------|
| 1. Cross-file 消失 | **有用** | 6/6 | 6/6 | 同等 |
| 2. CLI precedence | **有用** | 6/6 | 6/6 | 同等 |
| 3. Ref-file 指示欠落 | **有用** | 5/6 | 6/6 | Treatment やや安定 |
| 4. Codex bypass | **的外れ** | 5/6 | 3/6 | **Treatment で減少** (dispute 増加) |
| 5. Unknown phase | **冗長** | 3/6 | 0/6 | **Treatment で消滅** |
| 6. Parser 混在 | **冗長** | 1/6 | 0/6 | Treatment で消滅 |

### 有用率の比較

| 指標 | Baseline | Treatment |
|------|----------|-----------|
| 有用 テーマ検出率 | 3/6 (Theme 1,2,3) = 常に検出 | 3/6 (Theme 1,2,3) = 常に検出 |
| 的外れ テーマ検出率 | Theme 4: 5/6 | Theme 4: 3/6 (**改善**) |
| 冗長 テーマ検出率 | Theme 5: 3/6, Theme 6: 1/6 | **0/6** (**改善: 冗長排除**) |

**Treatment の改善点**:
1. **的外れ (Codex bypass) が40%減少** — role-specific context により reviewer が実装パスをより正確に推定
2. **冗長 (unknown phase, parser) が完全消滅** — role 分担意識が「自分のスコープ外」の低 impact 指摘を抑制
3. **findings 分散が半減** (sd=1.0→0.5) — ロール明確化により出力品質が安定

**Treatment の注意点**:
1. **info が3倍増** (1.0→3.0) — reviewer の orientation ノートが増加。有害ではないが冗長情報量が増加
2. **severity shift** — blocking が微減 (2.8→2.5)、advisory が微増 (0.5→0.8)。role 分担により severity 判定がやや保守的に

---

## GO/NO-GO 判定

計画の判定基準:
- **findings 総数が維持 (±20%)**: ✅ 3.3 vs 3.3 (±0%)
- **「有用」率が改善**: ⚠️ 有用テーマ検出率は Baseline と同等 (3/3 維持)。改善はノイズ削減側 (的外れ・冗長の減少) であり、有用 findings の増加ではない
- **「的外れ」率が悪化しない**: ✅ 的外れ (Codex bypass) が 83%→50% に改善

### Severity 突合検証

有用 Theme 1/2/3 が Treatment で blocking を維持しているか (suppression risk の排除):

| Theme | Baseline severity | Treatment severity | 判定 |
|-------|-------------------|-------------------|------|
| 1. Cross-file 消失 | 6/6 blocking | 6/6 blocking | ✅ 維持 |
| 2. CLI precedence | 6/6 blocking | 6/6 blocking | ✅ 維持 |
| 3. Ref-file 指示欠落 | 1 ADV + 4 BLO (5/6) | 4 ADV + 2 BLO (6/6) | ⚠️ 検出率は改善 (5→6)、severity は混在 (CC escalation で blocking 化) |

Theme 3 は両条件とも summarizer 段階では advisory/blocking が混在するが、cross-check phase で blocking に escalation される。Treatment が genuine blocking を見逃した (suppression) 証拠は **なし**。

n=6 は noise reduction の観察には十分だが、severity shift 等の下位指標で統計的に強い主張をするにはサンプル不足。この点は留意事項として記録する。

### 判定: **CONDITIONAL GO**

Treatment (--role-prompts) は Baseline と同等の findings 数・有用テーマ検出率を維持しつつ、的外れ・冗長 findings を減少させ、出力の安定性 (分散半減) を向上させた。有用率は「改善」ではなく「維持 + ノイズ削減」であるため CONDITIONAL。デフォルト ON 化 (Phase 3) の前提条件として、ベンチマークが検出した実バグ 3 件の修正が必要。

### Phase 3 前の修正事項 (MUST)

ベンチマークで全 trial が検出した **有用 findings** は、デフォルト ON 化の前提条件として修正すべき:

1. **[MUST] Theme 1: small phase ガード** — `resolvePrompts` に arch reviewer 有無のチェックを追加し、arch なし時は RolePrompts を適用しない
2. **[MUST] Theme 2: CLI flag tri-state** — `--role-prompts` / `--no-role-prompts` を独立 state に分離し precedence 違反を解消
3. **[MUST] Theme 3: ref-file 指示追加** — `AutoPhase*RefFilePrompt` に明示的な file read 指示を追加

これら3点の修正後、デフォルト ON 化を別 PR で実施可能。
