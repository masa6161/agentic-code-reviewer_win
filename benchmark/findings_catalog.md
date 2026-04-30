
## BASELINE (no-role-prompts)

### Summarizer Findings (20 unique across 6 trials)

**[BLO] F1**: AutoPhaseDiffPrompt cross-file suppression drops bugs in small/manual phases
- Trials: [1] (1/6) | rev=4 arch=1 diff=3
- AutoPhaseDiffPrompt tells diff reviewers to skip cross-file architectural concerns, but the `small` auto-phase (and manual `--phase small/medium/large`) has only diff reviewers, so cross-file bugs are silently dropped. Gate the skip clause to grouped-diff phases or retain cross-file coverage.

**[BLO] F2**: RolePrompts CLI default silently overrides env/config precedence
- Trials: [1] (1/6) | rev=2 arch=1 diff=1
- In cmd/acr/main.go:495, `RolePrompts: rolePrompts && !noRolePrompts` evaluates to false when neither flag is passed and overrides `ACR_ROLE_PROMPTS=true` / `role_prompts: true`, violating the documented flags > env > config precedence. Set flagValues only when `RolePromptsSet` is true, mirroring AutoPhase.

**[BLO] F3**: --no-role-prompts=false silently overrides ACR_ROLE_PROMPTS / config true
- Trials: [2] (1/6) | rev=2 arch=1 diff=1
- The pair-of-flags resolution at cmd/acr/main.go:495 marks RolePromptsSet=true even when the user passes `--no-role-prompts=false` (or `--role-prompts=false`), forcing the resolved value to false and overriding env/config truth without distinguishing explicit-disable from explicit-no-op.

**[BLO] F4**: AutoPhaseDiffPrompt suppresses cross-file findings in single-phase / small runs
- Trials: [2] (1/6) | rev=2 arch=1 diff=1
- AutoPhaseDiffPrompt is applied to any non-arch Phase via the default branch and instructs reviewers to skip cross-file architectural concerns; in single-phase small/flat runs no arch reviewer exists, so cross-file bugs are silently dropped — a coverage regression.

**[ADV] F5**: Ref-file role-prompt templates drop explicit "read this diff file" directive
- Trials: [2] (1/6) | rev=2 arch=1 diff=2
- AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt embed the diff path with "Review the code changes in %s" but omit the explicit instruction to read the file present in existing ref-file prompts; large-diff ref-file reviews may proceed without actually loading the diff contents.

**[BLO] F6**: CLI flag precedence bug: --no-role-prompts=false silently overrides ACR_ROLE_PROMPTS / yaml
- Trials: [3] (1/6) | rev=1 arch=1 diff=0
- cmd/acr/main.go:495 の `RolePrompts: rolePrompts && !noRolePrompts` と `RolePromptsSet = changed("role-prompts") || changed("no-role-prompts")` の組合せにより、`--no-role-prompts=false` の明示指定が `RolePromptsSet=true` を立てつつ `RolePrompts=false` を流し込み、env/yaml 設定を黙って打ち消す。precedence 契約 (flag > env > yaml) に違反。

**[BLO] F7**: Small/flat phase loses cross-file coverage under RolePrompts
- Trials: [3] (1/6) | rev=2 arch=1 diff=1
- review.go:827 の small/flat 経路は `Phase:"diff"` 単独で arch reviewer を持たないが、`AutoPhaseDiffPrompt` (および diff_review.go:30 の switching) は『Cross-file architectural concerns (the arch reviewer handles those)』を Skip と明記。RolePrompts=true の小規模レビューでクロスファイル欠陥が誰にもレビューされない構造的欠陥。arch reviewer の有無を Phase だけで判定できない。

**[BLO] F8**: Ref-file prompts drop explicit "read this file" instruction (diff and arch)
- Trials: [3] (1/6) | rev=2 arch=1 diff=1
- internal/agent/prompts.go:188 / :240 の AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt は ref-file 経路で diff 本文を埋め込まず、`%s` がパスである旨も「Read the file at %s」相当の明示指示も欠落。本文は "Review the code changes in %s" のみで、agent boundary を越えた暗黙の前提が壊れ、large auto-phase diff/arch レビューが実際の diff を読まず silent に走るおそれ。

**[BLO] F9**: Small-phase + RolePrompts: cross-file findings silently dropped
- Trials: [4] (1/6) | rev=3 arch=1 diff=2
- `AutoPhaseDiffPrompt` (internal/agent/prompts.go:165, :190) instructs reviewers to skip cross-file concerns because an arch reviewer is assumed, but `parsePhases("small")` (review.go:820–840) creates only diff reviewers over the full diff with no arch reviewer. With `RolePrompts=true` on small/auto-phase, cross-file findings are silently suppressed across all reviewers.

**[BLO] F10**: CLI flag precedence: --no-role-prompts=false silently overrides ACR_ROLE_PROMPTS=true
- Trials: [4] (1/6) | rev=1 arch=1 diff=0
- main.go:441 marks `RolePromptsSet=true` whenever either `--role-prompts` or `--no-role-prompts` was changed, and main.go:495 computes `RolePrompts: rolePrompts && !noRolePrompts`. Explicit `--no-role-prompts=false` therefore reports the flag as set with value `false` and overrides `ACR_ROLE_PROMPTS=true`, breaking the flags > env > yaml > defaults contract.

**[BLO] F11**: Ref-file prompt regression: AutoPhase ref-file variants drop 'Use the Read tool' directive
- Trials: [4] (1/6) | rev=1 arch=1 diff=0
- Existing ref-file prompts (`DefaultClaudeRefFilePrompt` prompts.go:66–69, `DefaultGeminiRefFilePrompt` L90–93, `DefaultCodexRefFilePrompt` L140–143) all explicitly tell the agent to read the diff file. New `AutoPhaseDiffRefFilePrompt` / `AutoPhaseArchRefFilePrompt` only say 'Review the code changes in %s', breaking the implicit ref-file contract and risking empty responses on the large-diff fallback path.

**[BLO] F12**: CLI flag synthesis silently overrides env/YAML for role-prompts
- Trials: [5] (1/6) | rev=1 arch=1 diff=0
- `RolePrompts: rolePrompts && !noRolePrompts` combined with `RolePromptsSet` based on either flag being changed forces a synthesized boolean onto Resolve, defeating `ACR_ROLE_PROMPTS=true` and `.acr.yaml: role_prompts: true`. Compute the resolved value only from the flag actually changed.

**[BLO] F13**: AutoPhaseDiffPrompt suppresses cross-file findings when no arch reviewer runs
- Trials: [5] (1/6) | rev=3 arch=1 diff=2
- `AutoPhaseDiffPrompt`/`AutoPhaseDiffRefFilePrompt` instruct the reviewer to skip cross-file concerns because an arch reviewer has the full diff, but in auto-phase small / `--phase small` / single-phase modes no arch reviewer exists, silently suppressing findings. Gate role prompts on arch presence or split into with-arch vs without-arch variants.

**[BLO] F14**: Auto-phase ref-file prompts omit explicit "read the diff file" directive
- Trials: [5] (1/6) | rev=1 arch=1 diff=0
- `AutoPhaseDiffRefFilePrompt`/`AutoPhaseArchRefFilePrompt` say "Review the code changes in %s …" without the explicit "Read the diff content from this file" directive that existing ref-file prompts use; some reviewer CLIs treat the path as metadata rather than opening it. This is a regression from the existing ref-file contract.

**[ADV] F15**: resolvePrompts default branch silently swallows unknown Phase values
- Trials: [5] (1/6) | rev=1 arch=1 diff=0
- `resolvePrompts` `default:` branch coerces every unknown `Phase` value (typos, future enum additions) into `AutoPhaseDiffPrompt`. Add an explicit `case "diff":` and either fall back to legacy prompts or return an error for unknown phases.

**[BLO] F16**: `--no-role-prompts=false` silently overrides env/YAML role_prompts:true
- Trials: [6] (1/6) | rev=2 arch=1 diff=1
- cmd/acr/main.go:495 で `RolePrompts: rolePrompts && !noRolePrompts` と `RolePromptsSet` の合成により、`--no-role-prompts=false` を渡すと `RolePromptsSet=true` かつ値が false となり、env/YAML の `role_prompts: true` を黙って上書きする。否定フラグの「未設定」と「明示 false」を区別できる経路（例: `noRolePromptsSet`）に分離すべき。

**[BLO] F17**: RolePrompts gating on Phase!="" suppresses cross-file findings on single-phase runs
- Trials: [6] (1/6) | rev=3 arch=1 diff=2
- internal/agent/diff_review.go の `if config.Phase != "" && config.RolePrompts` ゲートにより、`--phase small`/medium 等 arch reviewer 不在の単一フェーズ実行でも `AutoPhaseDiffPrompt` が適用される。プロンプト本文は「architecture reviewer が full diff を担当」と虚偽を述べ、diff reviewer の cross-file 指摘を抑制してカバレッジ穴を生む。runner から arch presence／真の auto-phase 起源を `ReviewConfig` に渡して区別すべき。

**[BLO] F18**: AutoPhase ref-file プロンプトに「ref ファイルから diff 本文を読み込め」指示が欠落
- Trials: [6] (1/6) | rev=2 arch=1 diff=1
- internal/agent/prompts.go の新規 `AutoPhaseDiffRefFilePrompt` / `AutoPhaseArchRefFilePrompt` は「Review the code changes in %s …」のみで、既存 RefFile 系にあった diff 本文読込の明示指示が落ちている。Phase=arch かつ `--role-prompts` ON の経路で agent が diff 本文をロードしないリスク（regression）。

**[BLO] F19**: arch/diff プロンプト出力形式混在によるパーサ互換性が未検証
- Trials: [6] (1/6) | rev=1 arch=1 diff=0
- arch プロンプトは `[must]/[imo]` 行頭、diff プロンプトは `file:line:` 形式と出力形式が混在しているが、本 diff 範囲ではパーサ側の変更がなく両形式の収集確証が取れない。少なくとも統合テストで両形式のアグリゲートを確認すべき。

**[ADV] F20**: 未知 Phase が `default:` で AutoPhaseDiffPrompt に黙って落ちる
- Trials: [6] (1/6) | rev=1 arch=1 diff=0
- internal/agent/diff_review.go:31 の `default:` で未知 Phase（例: `--phase smal` typo）を `AutoPhaseDiffPrompt` に黙ってフォールバックさせるため、意味が歪む。許可済み Phase を明示列挙し、未知時はエラーまたは legacy フォールバックにすべき。


### Cross-Check Findings (61 unique)

**[BLO/esc] CC1**: Small-phase role prompts issue is broader than any single file group
- Trials: [1] (1/6)
- The arch finding about diff prompts skipping cross-file concerns is corroborated independently from prompt text, prompt selection, and runner propagation across multiple diff groups, showing the bug s

**[BLO/gap] CC2**: Codex role-prompts no-op was not covered by diff findings
- Trials: [1] (1/6)
- The arch group reports that codex reviewers bypass the prompt-resolution path, but no succeeding diff group reports a corresponding issue despite g01 covering internal/agent/diff_review.go, leaving th

**[ADV/gap] CC3**: Unknown phase fallback was not covered by diff findings
- Trials: [1] (1/6)
- The arch group flags resolvePrompts treating any non-arch phase as diff, but no succeeding diff group reports or refutes that behavior even though g01 covered internal/agent/diff_review.go, leaving a 

**[BLO/esc] CC4**: AutoPhaseDiffPrompt cross-file suppression also impacts manual --phase small/medium/large
- Trials: [1] (1/6)
- Arch finding (id=2) framed the cross-file-skip regression as an auto-phase small-only issue. g01 (id=9) and g03 (id=12) confirm RolePrompts is selected for any non-arch phase and propagated by runner.

**[BLO/gap] CC5**: Codex reviewer no-op for --role-prompts not cross-validated by file-owning diff group
- Trials: [1] (1/6)
- Arch (id=1) flagged that codex bypasses executeDiffBasedReview, making --role-prompts a silent no-op for codex. g01 owns internal/agent/diff_review.go and cmd/acr/main.go where the flag is exposed wit

**[ADV/gap] CC6**: Ref-file arch path coverage gap
- Trials: [1] (1/6)
- Arch (id=5) raised that ref-file arch reviews may receive the generic Claude ref-file prompt with no arch framing — a pre-existing gap surfaced by this refactor. The ref-file path lives in internal/ag

**[BLO/esc] CC7**: Widespread architectural flaw: 'small' phase cross-file blind spot
- Trials: [1] (1/6)
- The design flaw where the 'small' auto-phase is incorrectly instructed to skip cross-file architectural concerns was independently detected by the arch review and all three diff groups across multiple

**[BLO/esc] CC8**: Confirmed configuration precedence bug
- Trials: [1] (1/6)
- Both the arch review and group g03 independently flagged the same boolean flag precedence bug in cmd/acr/main.go where environment variables are silently overridden by defaults. This corroboration con

**[BLO/gap] CC9**: Unaddressed Codex agent prompt bypass
- Trials: [1] (1/6)
- The arch review identified a blocking issue in internal/agent/diff_review.go where Codex agents silently bypass the role prompt logic. Group g01, assigned to review this file, failed to report or addr

**[BLO/gap] CC10**: Codex role-prompts path lacks diff-group confirmation
- Trials: [2] (1/6)
- The arch review reports that Codex reviewers bypass role prompt resolution, but the only diff group covering internal/agent/diff_review.go does not clearly surface this Codex-specific execution-path i

**[BLO/gap] CC11**: Unknown phase fallback lacks diff-group confirmation
- Trials: [2] (1/6)
- The arch review reports that unknown phase strings are silently coerced into diff-role behavior in internal/agent/diff_review.go. The diff group covering that file does not clearly confirm this bounda

**[ADV/gap] CC12**: Config boilerplate smell spans multiple diff groups
- Trials: [2] (1/6)
- The arch review identifies config flag addition as requiring coordinated edits across CLI/config/resolution layers. The relevant files are split between g01 and g02, so no single diff group has full c

**[ADV/gap] CC13**: Prompt selection ownership spans runner and agent boundaries
- Trials: [2] (1/6)
- The arch review questions placing prompt-template selection on agent.ReviewConfig, while the affected ownership boundary crosses internal/agent and internal/runner files reviewed by different diff gro

**[BLO/gap] CC14**: Codex agent path bypasses RolePrompts plumbing
- Trials: [2] (1/6)
- Architectural review (arch #2 / id=2) flags that Codex's review path never invokes resolvePrompts, while g01/g02 reviewed cmd/acr/main.go, internal/agent/diff_review.go, and prompts.go but produced no

**[BLO/esc] CC15**: AutoPhaseDiffPrompt suppresses cross-file findings in single-phase small runs
- Trials: [2] (1/6)
- Independently observed by arch (#4 / id=4) and g01 (id=14): the diff-role prompt instructs reviewers to skip cross-file concerns assuming an arch reviewer exists, but auto-phase small/flat mode runs w

**[BLO/esc] CC16**: --no-role-prompts=false silently overrides env/config true
- Trials: [2] (1/6)
- arch (#3 / id=3) and g01 (id=14) independently identify the same precedence bug at cmd/acr/main.go:495 where the pair-of-flags pattern marks RolePromptsSet true with value false, overriding ACR_ROLE_P

**[BLO/esc] CC17**: Ref-file prompts drop explicit diff-file read instruction
- Trials: [2] (1/6)
- arch (#6 / id=6, advisory) and g02 (id=11, advisory) both flag prompts.go:188/240 ref-file prompts as no longer instructing the agent to read the diff file. Two independent groups concur on a regressi

**[BLO/gap] CC18**: Unknown Phase string silently coerced to diff role
- Trials: [2] (1/6)
- arch (#5 / id=5) flags that internal/agent/diff_review.go:30-36 falls through the default arm for unknown Phase values, treating them as diff and dropping cross-file coverage. g01 owns diff_review.go 

**[ADV/gap] CC19**: RolePrompts field reaching runner not verified by runner-owning group
- Trials: [2] (1/6)
- g03 owns internal/runner/runner.go and runner_test.go but produced only process meta-commentary (id=0, id=1) with zero substantive findings on whether RolePrompts is actually propagated from runner.Co

**[BLO/gap] CC20**: Cross-group dependency for single-phase review bug
- Trials: [2] (1/6)
- The blocking bug where single-phase reviews silently skip cross-file concerns is split across group boundaries. It requires coordinated fixes in both `internal/agent/diff_review.go` (g01), which impro

**[ADV/esc] CC21**: Configuration layer boilerplate spans all diff groups
- Trials: [2] (1/6)
- The structural smell of adding a single boolean flag requiring coordinated edits spans across CLI/agent config (g01), core config (g02), and runner (g03). Viewing this across groups elevates it from a

**[ADV/gap] CC22**: Prompt resolution coupling across agent and runner layers
- Trials: [2] (1/6)
- The architectural suggestion to decouple prompt resolution from the review request payload requires cross-file changes between `internal/agent/config.go` (g01) and `internal/runner/runner.go` (g03).

**[BLO/gap] CC23**: CLI precedence regression lacked diff-group coverage
- Trials: [3] (1/6)
- The arch review identified a blocking flag/env/yaml precedence violation spanning CLI flag resolution and config state, but no succeeding diff group reported or validated that contract despite g01 cov

**[BLO/esc] CC24**: Small/flat role prompt coverage should be blocking
- Trials: [3] (1/6)
- g01 reported the small/flat diff prompt coverage loss as advisory, while the arch review confirms the same behavior creates a structural zero-coverage gap for cross-file defects when no arch reviewer 

**[BLO/esc] CC25**: Ref-file prompt regression affects both diff and arch paths
- Trials: [3] (1/6)
- g02 found the ref-file prompt no longer tells agents to read the temp diff file for both diff and arch reviews, corroborating the arch finding and broadening it from a prompt wording issue into a cros

**[BLO/esc] CC26**: Small/flat 経路の cross-file カバレッジ欠落: arch と g01 で severity 不一致
- Trials: [3] (1/6)
- ID 4 (arch, blocking) と ID 16 (g01, advisory) は同一の構造欠陥を指している。AutoPhaseDiffPrompt が「cross-file は arch reviewer 担当」と明記している一方、review.go:827 経路の small/flat モードは Phase="diff" の reviewer しか作らないため、RolePrompt

**[BLO/esc] CC27**: Ref-file プロンプトの read 指示欠落: arch と g02 で severity 不一致
- Trials: [3] (1/6)
- ID 5 (arch, blocking) と ID 12 (g02, advisory) は prompts.go:188/240 の AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt が「Read the file at %s」相当の明示指示を欠く同一問題を指す。arch は agent boundary を越えた暗黙前提崩壊として

**[BLO/gap] CC28**: --no-role-prompts=false の precedence 違反が g01/g02 境界に分割されて検出漏れ
- Trials: [3] (1/6)
- ID 3 (arch, blocking) の precedence バグは cmd/acr/main.go:495 (g01) と internal/config/config.go の FlagState/Resolve (g02) に跨る。g01 はフラグ宣言のみ、g02 は config 解決のみを見ており、両側を結合した contract 違反として検出できたのは arch のみ。グルー

**[ADV/gap] CC29**: config 層 bool フラグ追加の 9 箇所手書き編集が g03 でも未検出
- Trials: [3] (1/6)
- ID 8 (arch, advisory) は config.go の boilerplate 問題だが、g02 (config.go 担当) でも g03 (runner.go / config_test.go 担当) でも独立検出されていない。今後の bool フラグ追加で同種 silent override を量産するリスクが高く、diff レビューのカバレッジ盲点として残ったまま。help

**[ADV/gap] CC30**: RolePrompts 均一伝搬の Phase 別 override 余地欠如: g03 runner レベルで未検出
- Trials: [3] (1/6)
- ID 9 (arch, advisory) は runner.Config 経由で全 reviewer に均一伝搬する設計を指摘するが、g03 (runner.go 担当) は runner レベルの拡張性を独立評価しておらず、Phase 別 toggle の将来要件に対する設計余地不足を arch しか拾っていない。runner 担当グループでの再評価が望ましい。

**[BLO/gap] CC31**: Unaddressed RolePrompts flag precedence violation
- Trials: [3] (1/6)
- The 'arch' group identified a blocking precedence violation in `cmd/acr/main.go` where `--no-role-prompts=false` silently overrides environment and YAML settings. Group 'g01' reviewed this file but fa

**[BLO/esc] CC32**: Structural flaw in small/flat review phase handling
- Trials: [3] (1/6)
- Group 'g01' noted the auto-phase review behavior as an advisory concern (id:16). However, the 'arch' group (id:4) identified that this logic structurally eliminates all cross-file architectural defect

**[BLO/esc] CC33**: Ref-file prompt instruction omission escalation
- Trials: [3] (1/6)
- Group 'g02' reported the missing file read instructions in ref-file mode as an advisory issue (id:12). The 'arch' group (id:5) correctly assessed this as a blocking issue because it breaks the implici

**[ADV/gap] CC34**: Unhandled phase fallback gap
- Trials: [3] (1/6)
- The 'arch' group noted an advisory issue in `internal/agent/diff_review.go` where unknown phases silently fall through to the `diff` role. Group 'g01' reviewed this file but did not report or address 

**[BLO/esc] CC35**: Small-phase role prompts issue appears in both arch and diff review
- Trials: [4] (1/6)
- The arch review and two diff groups independently identify that diff prompts tell reviewers to skip cross-file concerns because an arch reviewer exists, while small-phase execution has no arch reviewe

**[BLO/gap] CC36**: CLI flag precedence bug was missed by responsible diff group
- Trials: [4] (1/6)
- The arch review reports a blocking flag/env precedence bug in cmd/acr/main.go, but the diff group covering cmd/acr/main.go did not report a matching finding. This is a coverage gap between the full-di

**[BLO/gap] CC37**: Ref-file prompt read-instruction regression was not covered by diff review
- Trials: [4] (1/6)
- The arch review reports that new ref-file role prompts omit the explicit instruction to read the diff file. The diff group covering internal/agent/prompts.go reported a different prompt-contract issue

**[ADV/gap] CC38**: Phase validation concern lacks file-scoped coverage
- Trials: [4] (1/6)
- The arch review flags silent fallback for unknown phase values in internal/agent/diff_review.go. The diff group covering that file did not report the validation issue, so the arch concern remains unad

**[BLO/esc] CC39**: Single-phase + RolePrompts cross-file blind spot confirmed across all three diff groups
- Trials: [4] (1/6)
- g01 (id=17), g02 (id=6), and arch (id=8) independently identified that AutoPhaseDiffPrompt instructs reviewers to skip cross-file concerns under the false premise that an arch reviewer is running, but

**[BLO/gap] CC40**: CLI flag precedence bug missed by g01 despite main.go being in scope
- Trials: [4] (1/6)
- The arch reviewer flagged a blocking trust-boundary violation at cmd/acr/main.go:441 / :495 where `--no-role-prompts=false` silently overrides ACR_ROLE_PROMPTS=true, breaking the documented flag>env>y

**[BLO/gap] CC41**: Ref-file prompt read-directive regression missed by g02 despite prompts.go being in scope
- Trials: [4] (1/6)
- Arch finding id=10 (blocking) shows AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt drop the explicit 'Read the file contents' directive that all three legacy ref-file prompts (Claude/Gemini/C

**[BLO/gap] CC42**: Missed CLI flag trust boundary violation
- Trials: [4] (1/6)
- The arch group identified a blocking defect in cmd/acr/main.go where a CLI flag silently overwrites an environment variable, but this was not reported by the g01 group responsible for reviewing the fi

**[BLO/gap] CC43**: Missed ref-file prompt regression
- Trials: [4] (1/6)
- The arch group flagged a blocking regression where read instructions are missing from ref-file prompts. This was not caught by the g02 group reviewing internal/agent/prompts.go.

**[ADV/gap] CC44**: Missed phase validation issue
- Trials: [4] (1/6)
- The arch group noted a missing validation for Phase values in internal/agent/diff_review.go, which was not addressed by the g01 group.

**[ADV/gap] CC45**: Missed domain model layering issue
- Trials: [4] (1/6)
- The arch group raised an architectural concern about mixing presentation state (RolePrompts) into the ReviewConfig domain type in internal/agent/config.go, which the g01 group did not report.

**[ADV/gap] CC46**: Cross-group structural debt in config layer
- Trials: [4] (1/6)
- The arch group identified structural debt requiring coordinated mechanical edits across configuration layers and flag parsing. This issue spans files distributed across g01, g02, and g03, making it in

**[BLO/gap] CC47**: CLI flag resolution bug lacks diff-group coverage
- Trials: [5] (1/6)
- The arch review reports a blocking configuration-resolution regression in cmd/acr/main.go, but the succeeding diff group covering cmd/acr/main.go/internal/agent/config.go did not report or address tha

**[BLO/gap] CC48**: Ref-file prompt contract regression lacks diff-group coverage
- Trials: [5] (1/6)
- The arch review reports that new ref-file prompts omit the explicit instruction to read diff content from the file, but the succeeding diff group covering internal/agent/prompts.go did not identify th

**[ADV/gap] CC49**: Unknown phase fallback concern lacks diff-group coverage
- Trials: [5] (1/6)
- The arch review reports that unknown phases are silently coerced to the diff prompt in internal/agent/diff_review.go, but the succeeding diff group covering that file did not report the phase-validati

**[ADV/gap] CC50**: Layering concern spans config, runner, and agent groups
- Trials: [5] (1/6)
- The arch review questions threading RolePrompts through config, runner, and agent layers; the relevant files are split across g01 and g03, so no single diff group covers the full cross-layer design co

**[BLO/esc] CC51**: Diff prompt suppression risk confirmed across groups
- Trials: [5] (1/6)
- The arch review and two separate diff groups independently identify that diff prompts tell reviewers to skip cross-file concerns even when no arch reviewer exists, so the advisory diff findings should

**[BLO/esc] CC52**: AutoPhaseDiffPrompt suppresses cross-file findings when no arch reviewer runs
- Trials: [5] (1/6)
- The arch group's blocking finding (id=3) about AutoPhaseDiffPrompt falsely advertising an arch reviewer is independently corroborated by g01 (id=11) and g02 (id=14) inspecting the diff-only call chain

**[ADV/gap] CC53**: Runner-layer validation of RolePrompts plumbing missing
- Trials: [5] (1/6)
- The arch group flagged the flag-precedence bug (id=2) and the layering concern of threading RolePrompts cmd→config→runner→agent (id=6), but g03 — the only group that owns runner.go and runner_test.go 

**[BLO/gap] CC54**: Ref-file prompt regression spans prompt definitions and their diff-review consumer without diff-group confirmation
- Trials: [5] (1/6)
- Arch finding id=4 identifies that AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt drop the explicit "read the diff content from this file" directive. The ref-file prompts live in g02's files (

**[BLO/gap] CC55**: Unreported CLI Flag Override Bug
- Trials: [5] (1/6)
- The architecture review identified a blocking bug in cmd/acr/main.go where explicit CLI flags silently override environment and YAML configuration values. The diff group reviewing this file missed the

**[BLO/gap] CC56**: Unreported Ref-File Prompt Regression
- Trials: [5] (1/6)
- The architecture review identified a blocking regression in internal/agent/prompts.go where ref-file prompts omit the explicit directive to read diff content, potentially breaking reviewer CLIs. The d

**[BLO/esc] CC57**: Systemic Auto-Phase Prompt Routing Bug
- Trials: [5] (1/6)
- The bug where diff reviewers are incorrectly instructed to skip cross-file concerns during small reviews spans multiple domains. It was identified in the prompt string definitions (g02) and in the rou

**[BLO/esc] CC58**: Role-prompt phase gating suppresses cross-file review outside grouped auto-phase
- Trials: [6] (1/6)
- The architecture review and two independent diff groups identify the same prompt-selection failure across agent and runner paths: diff reviewers can be told an architecture reviewer covers cross-file 

**[BLO/gap] CC59**: Parser compatibility for mixed arch and diff output formats is unverified
- Trials: [6] (1/6)
- The architecture review flags that arch prompts emit [must]/[imo] while diff prompts emit file:line findings, but no succeeding diff group covers parser or aggregation behavior for mixed formats. This

**[BLO/esc] CC60**: RolePrompts suppresses cross-file review on single-phase runs
- Trials: [6] (1/6)
- The 'arch' and 'g01' groups found that AutoPhaseDiffPrompt tells single-phase reviewers (e.g., 'small') to skip cross-file issues. Group 'g03' identified the root cause in runner.go, which propagates 

**[BLO/gap] CC61**: CLI flag --no-role-prompts=false silently overrides configuration
- Trials: [6] (1/6)
- The 'arch' and 'g03' groups identified a bug in cmd/acr/main.go where passing --no-role-prompts=false incorrectly overrides YAML/env configurations. However, cmd/acr/main.go was assigned to 'g01', whi


## TREATMENT (role-prompts)

### Summarizer Findings (20 unique across 6 trials)

**[BLO] F1**: AutoPhaseDiffPrompt drops cross-file coverage in small/flat phase when --role-prompts is on
- Trials: [1] (1/6) | rev=3 arch=1 diff=2
- When RolePrompts is enabled, any non-arch phase (including small/flat with Phase="diff") routes through AutoPhaseDiffPrompt, whose body explicitly tells reviewers to skip cross-file/architectural concerns. Small/flat runs have no arch reviewer, so cross-file bugs get silently dropped; multi-reviewer confirmed.

**[BLO] F2**: Codex execution path may bypass resolvePrompts (disputed)
- Trials: [1] (1/6) | rev=2 arch=1 diff=1
- One reviewer reports that resolvePrompts is only invoked inside executeDiffBasedReview (claimed Claude/Gemini-only), so --role-prompts silently no-ops for any reviewer matrix that includes Codex. Another reviewer counters that with a non-empty phase, Codex also enters the diff-based path and the propagation does reach runner; the contradiction needs source verification before closing.

**[BLO] F3**: --no-role-prompts=false silently overrides env/yaml-enabled RolePrompts
- Trials: [1] (1/6) | rev=1 arch=1 diff=0
- loadAndResolveConfig sets RolePromptsSet=true if either flag was changed and writes flagValues.RolePrompts = rolePrompts && !noRolePrompts. Passing only --no-role-prompts=false yields false, and Resolve's flag tier then overwrites a true value coming from env/yaml. Treat the two flags as separate tri-state, or only mark RolePromptsSet when intent is unambiguous.

**[ADV] F4**: Ref-file role prompts no longer instruct the agent to read the diff file
- Trials: [1] (1/6) | rev=2 arch=1 diff=1
- In ref-file mode the diff is not embedded in stdin and the agent must be told to open the referenced file. The new role prompts at internal/agent/prompts.go:188 (diff) and :240 (arch) drop the explicit "this is a diff file you must read" instruction, and the legacy path also leaves RefFilePrompt untouched when Phase=="arch" with RolePrompts=false — so a ref-file arch run gets the diff prompt's read-instructions instead of arch ones.

**[BLO] F5**: CLI flag composition: --no-role-prompts=false silently overrides ACR_ROLE_PROMPTS=true
- Trials: [2] (1/6) | rev=1 arch=1 diff=0
- In cmd/acr/main.go, RolePromptsSet treats --no-role-prompts as a flag-source signal while RolePrompts is computed as `rolePrompts && !noRolePrompts`. With ACR_ROLE_PROMPTS=true, passing --no-role-prompts=false flips the env-derived true to false — a precedence design bug.

**[BLO] F6**: AutoPhaseDiffPrompt suppresses cross-file findings in single-phase (small/flat) auto-phase runs
- Trials: [2] (1/6) | rev=1 arch=1 diff=0
- resolvePrompts' default branch selects AutoPhaseDiffPrompt even when no arch reviewer exists (small/flat phase). That prompt's "Skip: Cross-file architectural concerns" directive then silently drops cross-file issues — the phase precondition and prompt wording are misaligned.

**[ADV] F7**: Ref-file auto-phase prompts omit explicit "read this diff file" directive (diff and arch)
- Trials: [2] (1/6) | rev=2 arch=1 diff=1
- AutoPhaseDiffRefFilePrompt and AutoPhaseArchRefFilePrompt only say "Review the code changes in %s" without telling the agent that %s is a diff file or that it must read it; executeDiffBasedReview does not embed the diff on this path, so reviewers may answer from prompt text alone and miss the actual changes. Independently confirmed by two groups, escalating to blocking.

**[BLO] F8**: --no-role-prompts=false silently overrides ACR_ROLE_PROMPTS=true
- Trials: [3] (1/6) | rev=1 arch=1 diff=0
- RolePrompts: rolePrompts && !noRolePrompts combined with RolePromptsSet = Changed("role-prompts") || Changed("no-role-prompts") allows --no-role-prompts=false to mark the flag as set and force RolePrompts=false at the highest precedence, violating the flag/env precedence contract.

**[BLO] F9**: AutoPhaseDiffPrompt subset directive misapplied to single-phase runs
- Trials: [3] (1/6) | rev=2 arch=1 diff=1
- resolvePrompts (internal/agent/diff_review.go:30) routes any non-"arch" Phase to AutoPhaseDiffPrompt, which states the reviewer sees only a subset and that an arch reviewer has the full diff. In auto-phase small / flat medium fallback the reviewer actually sees the full diff and there is no arch reviewer, so cross-file findings are suppressed by a misleading directive.

**[ADV] F10**: Ref-file prompts dropped explicit "Read this file" instruction
- Trials: [3] (1/6) | rev=1 arch=1 diff=0
- AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt no longer carry the explicit read-the-file directive that distinguished ref-file mode from inline-diff mode, so agents may not realize they must open %s instead of expecting inline diff content. Cross-check escalated this to blocking because the regression spans prompts and diff_review consumers.

**[BLO] F11**: --role-prompts フラグ precedence の設計破綻
- Trials: [4] (1/6) | rev=1 arch=1 diff=0
- `--role-prompts` と `--no-role-prompts` の値を `rolePrompts && !noRolePrompts` で先に合成するため、`ACR_ROLE_PROMPTS=true` 下で `--no-role-prompts=false` を渡すと `RolePromptsSet=true / RolePrompts=false` が合成されて env を誤って上書きする。フラグペアは独立 state として保持し、`Changed` を個別検査して合成を禁止すべき。

**[BLO] F12**: auto-phase small で AutoPhaseDiffPrompt が cross-file 所見を抑制
- Trials: [4] (1/6) | rev=2 arch=1 diff=1
- `parsePhases("small", N)` は arch reviewer を含まない単相 (`Phase:"diff"`) を生成するのに、`AutoPhaseDiffPrompt` は「architecture reviewer has the full diff」と断言し、cross-file 所見を arch にゆだねるよう LLM に誤誘導する。`resolvePrompts` で単相/複相を区別するか、`RolePrompts` を複相時のみ有効化するゲートが必要。`Phase != ""` だけで RolePrompts を選択する diff_review.go:30 のロジックも同根。

**[ADV] F13**: AutoPhase ref-file 系プロンプトが diff ファイル読込指示を欠落
- Trials: [4] (1/6) | rev=1 arch=0 diff=1
- `AutoPhaseDiffRefFilePrompt` (prompts.go:188) と `AutoPhaseArchRefFilePrompt` (prompts.go:240) は ref-file モードで diff を temp file に書き出した後に渡されるにも関わらず、`%s` が diff ファイルパスであることや読み込めという指示を欠いており、ref-file mode で空/不正なレビューや未読のまま arch 判定が走る恐れがある。

**[ADV] F14**: arch + RolePrompts=false 経路で RefFilePrompt が diff 用のまま残る非対称
- Trials: [4] (1/6) | rev=1 arch=1 diff=0
- `resolvePrompts` の `Phase=="arch" && RolePrompts==false` 分岐は `DefaultPrompt` のみ `DefaultArchPrompt` に差し替え、`RefFilePrompt` はエージェント別の diff 用テンプレートのまま放置する。`TestResolvePrompts_RolePromptsDisabled_ArchPhase` がこの非対称を固定しているが、`--arch-reviewer-agent` を ref-file モードで動かすと `--no-role-prompts` 指定時に arch reviewer が diff 用 ref-file プロンプトを受け取り、本機能が塞ぎたかったカバレッジ穴と同型のギャップが残る。

**[BLO] F15**: --role-prompts / --no-role-prompts CLI フラグ合成で env/yaml の true を黙って false に上書き
- Trials: [5] (1/6) | rev=2 arch=1 diff=1
- `RolePromptsSet` を両フラグの Changed で true にしつつ値を `rolePrompts && !noRolePrompts` で算出しているため、`--no-role-prompts=false` 等で `ACR_ROLE_PROMPTS=true` や yaml `role_prompts: true` が無効化される。tri-state 合成にすべき。

**[BLO] F16**: AutoPhaseDiffPrompt が arch reviewer 不在のフェーズでも cross-file findings を抑制
- Trials: [5] (1/6) | rev=3 arch=1 diff=2
- `AutoPhaseDiffPrompt` が「arch reviewer が full diff を持つ」前提で cross-file 指摘をスキップさせるが、small/flat 単一フェーズや明示 `--phase small/medium` でも適用されるため、arch reviewer のいない実行で coverage gap が生じる。multi-phase かつ arch フェーズ存在時に限定すべき。

**[BLO] F17**: AutoPhase*RefFilePrompt が「diff ファイルを読め」指示を欠落 (ref-file 契約退行)
- Trials: [5] (1/6) | rev=2 arch=1 diff=1
- 既存の ref-file プロンプトに含まれる「Read the diff content from this file」指示が新しい `AutoPhaseDiffRefFilePrompt` / `AutoPhaseArchRefFilePrompt` で抜け落ちており、CLI が `%s` を metadata として扱うと diff を読まずにレビューする恐れがある。既存契約に対する regression。

**[BLO] F18**: --no-role-prompts=false silently overrides ACR_ROLE_PROMPTS / yaml role_prompts
- Trials: [6] (1/6) | rev=2 arch=1 diff=1
- cmd/acr/main.go:495 collapses --role-prompts/--no-role-prompts into a single boolean before precedence resolution, so an explicit --no-role-prompts=false (or --role-prompts=false) is marked 'set' yet resolves to false, overriding env/yaml. Needs tri-state or separate set/value tracking.

**[BLO] F19**: AutoPhaseDiffPrompt suppresses cross-file findings in single-phase runs
- Trials: [6] (1/6) | rev=2 arch=1 diff=1
- internal/agent/prompts.go AutoPhaseDiffPrompt asserts an arch reviewer holds the full diff and tells reviewers to skip cross-file concerns, but resolvePrompts activates it for any non-arch Phase including single-phase small/medium runs where no arch reviewer exists, regressing coverage when RolePrompts=true outside grouped mode.

**[BLO] F20**: AutoPhase RefFile prompts drop 'Read the diff content from %s' directive
- Trials: [6] (1/6) | rev=1 arch=1 diff=0
- AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt omit the explicit 'Read the diff content from the file at %s' directive present in every other *RefFilePrompt constant, leaving only 'Review the code changes in %s' which lets the LLM skip reading the reference file. Contract regression across the ref-file prompt family.


### Cross-Check Findings (64 unique)

**[BLO/con] CC1**: Codex prompt-path coverage is disputed
- Trials: [1] (1/6)
- The arch review says Codex bypasses resolvePrompts and therefore role prompts silently no-op, while g02 says Codex reaches the diff-based path when phase is set. These cannot both describe the same ex

**[BLO/gap] CC2**: CLI negation flag precedence lacks diff-group confirmation
- Trials: [1] (1/6)
- The arch review reports a blocking configuration-resolution bug where --no-role-prompts=false can override env/yaml truthy settings, but no succeeding diff group reports validating or refuting that sp

**[BLO/esc] CC3**: Small or flat review coverage loss is cross-group confirmed
- Trials: [1] (1/6)
- The same role-prompt coverage loss appears from both prompt-selection behavior and prompt text: arch flags non-arch phases falling into the diff prompt, g01 flags RolePrompts applying to small/flat di

**[ADV/con] CC4**: ReviewOpts wiring concern is resolved by diff groups
- Trials: [1] (1/6)
- The arch review raised a possible gap that ReviewOpts might not carry RolePrompts into execution, but g02 and g03 independently report that RolePrompts reaches the runner and agent ReviewConfig. This 

**[BLO/con] CC5**: Codex routing claim contradicts subset-level evidence
- Trials: [1] (1/6)
- Arch group claims `--role-prompts` silently no-ops for Codex because it bypasses `resolvePrompts`; g02 group, after reading the actual code, asserts Codex DOES enter the diff-based path when `Phase` i

**[BLO/esc] CC6**: AutoPhaseDiffPrompt drops cross-file coverage in small/flat phase (multi-group confirmed)
- Trials: [1] (1/6)
- Three independent groups (arch full-diff review, g01 reviewing diff_review.go, g02 reviewing prompts.go) independently identified the same defect: `AutoPhaseDiffPrompt` instructs diff reviewers to ski

**[BLO/esc] CC7**: Ref-file prompt regression in role-prompts mode (cross-group confirmed)
- Trials: [1] (1/6)
- Arch group flagged that the legacy ref-file path is left inconsistent under role-prompts (advisory); g02, reading `prompts.go:188` directly, independently observed that the ref-file prompt no longer t

**[BLO/gap] CC8**: CLI flag composition bug not corroborated by group owning cmd/acr/main.go
- Trials: [1] (1/6)
- Arch flagged a blocking precedence bug where `rolePrompts && !noRolePrompts` lets `--no-role-prompts=false` clobber a true value from env/yaml. The group that actually owns `cmd/acr/main.go` (g01) rev

**[ADV/gap] CC9**: g03 produced no substantive findings on runner/config_test wiring
- Trials: [1] (1/6)
- g03 owned `internal/runner/runner.go`, `runner_test.go`, and `config_test.go` — exactly the layer where `RolePrompts` propagation must be verified end-to-end (arch advisory #4 specifically asks whethe

**[BLO/con] CC10**: Codex execution path contradiction
- Trials: [1] (1/6)
- The arch group claims the Codex agent bypasses `resolvePrompts` and the diff-based path entirely, whereas g02 claims Codex does enter the diff-based path when a phase is attached.

**[ADV/con] CC11**: Flag propagation confirmation contradiction
- Trials: [1] (1/6)
- The arch group raised a concern that `RolePrompts` might not be properly populated in `ReviewOpts` to reach the runner, but g03 contradicts this by confirming the flag value is successfully carried th

**[BLO/esc] CC12**: AutoPhaseDiffPrompt skips cross-file concerns in flat phase
- Trials: [1] (1/6)
- Independently identified across multiple groups: routing all non-'arch' phases to `AutoPhaseDiffPrompt` causes reviewers to silently skip cross-file and architectural concerns during small or flat dif

**[BLO/gap] CC13**: Missed CLI boolean logic bug in target files
- Trials: [1] (1/6)
- The arch group found a critical bug where `--no-role-prompts=false` silently disables the role prompts feature due to faulty boolean logic. This was entirely missed by g01, whose target files included

**[BLO/gap] CC14**: Role prompt behavior is only covered by the arch review
- Trials: [2] (1/6)
- The arch review reports that `--role-prompts` is not applied consistently across reviewer execution paths, but no diff group produced a corresponding code-level finding for the cross-file path from CL

**[BLO/gap] CC15**: Config flag precedence bug was not picked up by the config diff groups
- Trials: [2] (1/6)
- The arch review identifies a CLI/env/config merge precedence bug for `--no-role-prompts=false` under `ACR_ROLE_PROMPTS=true`, but the diff groups covering `cmd/acr/main.go` and `internal/config/config

**[BLO/gap] CC16**: Phase-to-prompt responsibility boundary lacks diff-group coverage
- Trials: [2] (1/6)
- The arch review reports that default prompt selection can silently drop cross-file review coverage and that unknown phase values fall through to diff prompts, but the diff groups did not produce match

**[BLO/esc] CC17**: Ref-file prompt regression is more severe across arch and diff contexts
- Trials: [2] (1/6)
- Both the arch review and g02 identify that ref-file auto-phase prompts fail to instruct agents to read the diff file. g02 shows the problem affects both diff and arch ref-file prompts, so the advisory

**[BLO/gap] CC18**: Codex agent path not covered by any diff group
- Trials: [2] (1/6)
- Arch finding #0 identifies that Codex bypasses resolvePrompts entirely, but no diff group reviewed internal/agent/codex.go — it is absent from g01/g02/g03 target_files. The blocking claim cannot be cr

**[BLO/gap] CC19**: g01 missed all blocking issues within its own file scope
- Trials: [2] (1/6)
- g01 reviewed cmd/acr/main.go and internal/agent/diff_review.go and returned only meta/advisory notes. Arch found four blocking defects (#0 Codex bypass site, #1 flag composition, #2 AutoPhaseDiff cros

**[BLO/esc] CC20**: Ref-file prompt regression independently confirmed across groups
- Trials: [2] (1/6)
- Arch #4 (advisory) and g02 #14 (advisory) independently identify the same regression: AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt no longer instruct the agent that %s is a diff file or tha

**[ADV/gap] CC21**: Runner-level RolePrompts propagation unverified by g03
- Trials: [2] (1/6)
- g03 covered internal/runner/runner.go and reported 'No code-level issues found,' but its meta notes show it explicitly excluded the test files from analysis on request. Combined with arch #0 (Codex pa

**[BLO/gap] CC22**: Codex Agent Silent RolePrompts Failure
- Trials: [2] (1/6)
- Arch finding (0) identified that Codex agents ignore config.RolePrompts, rendering the feature a no-op for them. Diff group g01, responsible for internal/agent/diff_review.go, failed to identify this 

**[BLO/gap] CC23**: CLI Flag Precedence Bug Missed
- Trials: [2] (1/6)
- Arch finding (1) identified a flag resolution bug in cmd/acr/main.go where flag state incorrectly overrides env variables when --no-role-prompts=false. Diff group g01 failed to catch this bug.

**[BLO/gap] CC24**: Silent Misclassification in resolvePrompts Missed
- Trials: [2] (1/6)
- Arch findings (2, 3) highlighted that missing or unknown phase values in resolvePrompts default to AutoPhaseDiff, which silently drops cross-file concerns if no arch reviewer exists. Diff group g01, c

**[BLO/esc] CC25**: Ref-File Prompt Read Instruction Missing
- Trials: [2] (1/6)
- Both Arch (4) and Diff group g02 (14) independently identified that prompts in prompts.go fail to instruct the agent to actually read the referenced diff file. This confirmed regression should be esca

**[BLO/gap] CC26**: Codex reviewer path was not covered by any diff group
- Trials: [3] (1/6)
- The arch review reports that the Codex reviewer bypasses the shared prompt-resolution path, but no succeeding diff group includes internal/agent/codex.go, so the agent-specific failure mode was only s

**[BLO/gap] CC27**: Flag precedence bug spans CLI and config/test boundaries
- Trials: [3] (1/6)
- The arch review reports an explicit false flag overriding ACR_ROLE_PROMPTS=true. That behavior crosses cmd/acr flag-state construction and internal/config resolution/tests, but no diff finding covers 

**[BLO/esc] CC28**: Misleading diff prompt is confirmed across arch and diff review
- Trials: [3] (1/6)
- Both the full-diff arch lane and a subset diff lane identify that non-arch phases can receive the subset-oriented AutoPhaseDiffPrompt. Because this prompt-selection issue affects full-diff auto-phase 

**[BLO/gap] CC29**: Codex agent bypasses RolePrompts plumbing — feature is silently agent-dependent
- Trials: [3] (1/6)
- The arch reviewer flagged that codex.go builds its own command and never routes through executeDiffBasedReview/resolvePrompts. None of the diff groups (g01 covering codex.go's caller surface, g02 cove

**[BLO/esc] CC30**: AutoPhaseDiffPrompt subset directive applied to single-phase runs — cross-file findings suppressed
- Trials: [3] (1/6)
- Both the arch reviewer (id 2) and g01's diff reviewer (id 13) independently identified that resolvePrompts applies AutoPhaseDiffPrompt to any non-arch Phase value, including auto-phase small/flat runs

**[BLO/gap] CC31**: CLI flag precedence violation spans config and CLI layers
- Trials: [3] (1/6)
- Arch finding id 1 identifies that --no-role-prompts=false silently overrides ACR_ROLE_PROMPTS=true via the RolePromptsSet computation. The CLI composition lives in g01's slice (cmd/acr/main.go, cmd/ac

**[BLO/esc] CC32**: Ref-file prompt regression spans prompts and diff_review consumers
- Trials: [3] (1/6)
- Arch finding id 4 (advisory) reports that AutoPhaseDiffRefFilePrompt/AutoPhaseArchRefFilePrompt drop the explicit 'Read this file' instruction. The prompt definitions live in g02 (internal/agent/promp

**[BLO/gap] CC33**: Config Precedence Bug Missed by Diff Group
- Trials: [3] (1/6)
- The arch group identified a blocking flag composition bug where `--no-role-prompts=false` silently overrides `ACR_ROLE_PROMPTS=true` (id 1). Group g02 reviewed `internal/config/config.go` but failed t

**[ADV/gap] CC34**: Prompt Instruction Regression Missed by Diff Group
- Trials: [3] (1/6)
- The arch group found that `AutoPhaseDiffRefFilePrompt` and `AutoPhaseArchRefFilePrompt` drop the explicit 'Read this file' instruction (id 4). Group g02 reviewed `internal/agent/prompts.go` but did no

**[BLO/esc] CC35**: Subset Prompt Incorrectly Applied to Full Diff
- Trials: [3] (1/6)
- Both the arch group (id 2) and diff group g01 (id 13) independently identified that `resolvePrompts` incorrectly applies the subset prompt to full-diff reviewers when the phase is not 'arch', which wi

**[BLO/gap] CC36**: Codex Agent Bypass Unaddressed in Code Changes
- Trials: [3] (1/6)
- The arch group noted that the Codex agent entirely bypasses the new `RolePrompts` logic because it builds its own command (id 0). This cross-file architectural issue is a gap as it is not addressed or

**[BLO/esc] CC37**: Single-phase auto-phase prompt suppression is under-severed in diff review
- Trials: [4] (1/6)
- g01 reports that RolePrompts is selected for any non-empty Phase and can suppress cross-file findings, while arch independently confirms the same small-auto-phase path lacks an architecture reviewer a

**[BLO/gap] CC38**: Role-prompts flag precedence bug was missed by the owning diff group
- Trials: [4] (1/6)
- arch reports a blocking precedence bug in cmd/acr/main.go, which is inside g01's target set. g01 reviewed the same config wiring area but only concluded the flag/env/yaml precedence followed existing 

**[BLO/esc] CC39**: Ref-file prompt regressions span arch and diff prompt modes
- Trials: [4] (1/6)
- g02 reports that the new auto-phase ref-file prompts omit instructions to read the diff file, while arch separately reports an existing arch-phase ref-file prompt asymmetry when role prompts are disab

**[BLO/esc] CC40**: AutoPhaseDiffPrompt single-phase coverage gap independently confirmed by arch and diff reviewers
- Trials: [4] (1/6)
- Arch (full-diff) review and g01 (cmd/acr/review.go + diff_review.go scope) independently identified that RolePrompts activates whenever Phase != "", including auto-phase=small single-phase runs that h

**[BLO/gap] CC41**: CLI flag precedence bug missed by file-scoped diff group
- Trials: [4] (1/6)
- Arch finding 12 identifies that `RolePromptsSet = rolePrompts && !noRolePrompts` corrupts precedence (e.g., `ACR_ROLE_PROMPTS=true` + `--no-role-prompts=false` silently disables). g01 reviewed cmd/acr

**[BLO/gap] CC42**: Layering violation spans all three diff groups but caught by no diff group
- Trials: [4] (1/6)
- Arch finding 14 flags that `RolePrompts` threads through cmd→config→runner→agent (every diff group's territory) yet none of g01/g02/g03 raised the cross-cutting placement issue, because each group onl

**[ADV/gap] CC43**: Pre-existing ref-file asymmetry on arch+RolePrompts=false path not surfaced by diff groups
- Trials: [4] (1/6)
- Arch finding 15 notes that when `Phase=="arch" && RolePrompts==false`, only DefaultPrompt is swapped to DefaultArchPrompt while RefFilePrompt remains the diff-style template — leaving an arch reviewer

**[ADV/gap] CC44**: Unknown Phase silent fallback to diff prompt missed by diff_review.go reviewer
- Trials: [4] (1/6)
- Arch finding 17 highlights that `resolvePrompts`' `default:` branch silently treats unknown Phase values as diff. g01 owned diff_review.go and surfaced an adjacent concern (RolePrompts gates on any no

**[BLO/esc] CC45**: AutoPhase ref-file prompt regression compounds single-phase coverage suppression
- Trials: [4] (1/6)
- g02 finding 3 shows AutoPhaseDiffRefFilePrompt / AutoPhaseArchRefFilePrompt drop the explicit "read this file" directive that existing ref-file prompts carry. Combined with arch/g01 finding (13/10) th

**[BLO/con] CC46**: Contradictory assessment of --role-prompts flag precedence logic
- Trials: [4] (1/6)
- Group arch (finding 12) identifies a critical logic bug in cmd/acr/main.go where boolean flag composition breaks precedence for --no-role-prompts overrides. Group g01 (finding 9) contradicts this, sta

**[BLO/gap] CC47**: Cross-group gap for single-phase prompt mismatch resolution
- Trials: [4] (1/6)
- Both arch (finding 13) and g01 (finding 10) identify that in single-phase runs (like auto-phase small), diff reviewers receive a prompt falsely assuming an architecture reviewer is present. Fixing thi

**[ADV/gap] CC48**: Configuration layering violation spans across all diff groups
- Trials: [4] (1/6)
- Arch finding 14 notes that RolePrompts is passed unmodified through many layers (ResolvedConfig, ReviewOpts, runner.Config, ReviewerSpec, ReviewConfig) instead of being resolved early. Because these l

**[BLO/esc] CC49**: Role-prompt coverage loss spans prompt text and activation conditions
- Trials: [5] (1/6)
- The same coverage-loss risk appears in both the diff prompt wording and the broader activation path: g02 flags the prompt suppressing cross-file review, while g01 shows RolePrompts can apply to full-d

**[BLO/gap] CC50**: Output-format parser impact is outside diff-group coverage
- Trials: [5] (1/6)
- The arch review flags mixed finding formats across phases, but the succeeding diff groups only cover prompt/config/runner surfaces and do not cover downstream parser or aggregation code that would pro

**[BLO/esc] CC51**: AutoPhaseDiffPrompt scope wider than auto-phase mode
- Trials: [5] (1/6)
- Arch group flagged AutoPhaseDiffPrompt suppressing cross-file findings in single-phase auto runs (id 3). g01's review of diff_review.go (id 16) extends this: resolvePrompts gates on any non-empty Phas

**[BLO/gap] CC52**: Output-format inconsistency unverified at parser layer
- Trials: [5] (1/6)
- Arch group raised that AutoPhaseArchPrompt mandates [must]/[imo] prefixes while AutoPhaseDiffPrompt keeps file:line format (id 8). No diff group covered the downstream parser/aggregator code path (par

**[ADV/gap] CC53**: Runner-layer prompt resolution path uncovered
- Trials: [5] (1/6)
- Arch group's layering finding (id 5) argues prompt resolution belongs in runner/review.go rather than agent.ReviewConfig. g03 covered runner.go but only confirmed RolePrompts is propagated through (id

**[BLO/esc] CC54**: RolePrompts improperly applied to standard explicit phases
- Trials: [5] (1/6)
- g01 identifies that diff_review.go applies RolePrompts to any non-empty Phase (including explicit ones like small/medium). g02 and arch identify that the resulting AutoPhaseDiffPrompt falsely tells th

**[BLO/gap] CC55**: Diff subset missed blocking prompt-parser contract regression
- Trials: [6] (1/6)
- The arch review flagged that the new arch prompt output format no longer matches downstream parsers, but the diff group owning internal/agent/prompts.go did not report it; this leaves a full-diff bloc

**[BLO/gap] CC56**: Diff subset missed ref-file prompt contract regression
- Trials: [6] (1/6)
- The arch review flagged that the new auto-phase ref-file prompts dropped the explicit instruction to read the referenced diff file, but the diff group owning internal/agent/prompts.go did not report i

**[BLO/esc] CC57**: G01 findings should inherit arch-level blocking severity
- Trials: [6] (1/6)
- G01 independently confirms the flag precedence bug and the diff-only role prompt coverage regression that the arch review marked blocking, but G01 recorded them only as advisory; cross-group agreement

**[BLO/gap] CC58**: Parser compatibility with arch prompt's new output format unverified across groups
- Trials: [6] (1/6)
- Arch finding 6 flags that AutoPhaseArchPrompt mandates `[must]`/`[imo]` prefixes while AutoPhaseDiffPrompt mandates `file:line:`, but the per-agent review parsers in internal/agent/*_review_parser.go 

**[BLO/esc] CC59**: Compound failure mode: flag-synthesis bug + single-phase prompt suppression
- Trials: [6] (1/6)
- Finding 4 (cmd/acr/main.go flag synthesis cannot distinguish 'set' from 'value') and finding 5 (AutoPhaseDiffPrompt suppresses cross-file findings in single-phase runs) interact. When the synthesis bu

**[ADV/gap] CC60**: g02 owns prompts.go but produced no concrete findings on the new prompt constants
- Trials: [6] (1/6)
- g02's target_files include internal/agent/prompts.go and prompts_test.go where the four new AutoPhase* constants live. Arch produced three blocking prompt-level findings (5, 6, 7) and one advisory (10

**[ADV/gap] CC61**: g03 declares 'no code-level issues' for runner.go path that propagates the buggy flag
- Trials: [6] (1/6)
- g03 finding 3 reports no production-code issues, observing only that runner.go threads RolePrompts into agent.ReviewConfig. However, runner.go is the propagation path for the flag-synthesis bug (findi

**[BLO/gap] CC62**: Unaddressed blocking regressions in prompts.go
- Trials: [6] (1/6)
- The arch review identified critical regressions in internal/agent/prompts.go, including output format mismatches that break parsers and dropped file read directives. Group g02, which was assigned this

**[BLO/esc] CC63**: Cross-group dependency for role prompt fallback bug
- Trials: [6] (1/6)
- The arch review flagged a cross-file bug where internal/agent/prompts.go (assigned to g02) assumes an arch reviewer exists, but internal/agent/diff_review.go (assigned to g01) activates this prompt ev

**[ADV/gap] CC64**: Unaddressed architectural layering and design concerns
- Trials: [6] (1/6)
- The arch review identified advisory issues regarding CLI logic leaking into internal/agent/config.go, overly broad fallback logic in internal/agent/diff_review.go, and unrelated changes in AGENTS.md. 

