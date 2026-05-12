# ACR Review

ACR (Agentic Code Reviewer) を実行し、マルチエージェントレビュー結果を構造化レポートとして報告する。

2つのモードがある:
- **単発レビュー**（デフォルト）: ACR を1回実行し、findings をレポートして終了
- **レビューゲート** (`--gate`): ACR 実行 → blocking findings の修正 → 再 ACR を verdict が "blocking" でなくなるまで反復

## パラメータ

### スキルワークフローパラメータ

| パラメータ | デフォルト | 説明 |
|-----------|-----------|------|
| `--gate` | false | 反復レビューゲートモードを有効化 |
| `--max-iter` | 3 | ゲートモード最大反復回数 |

### ACR パススルーパラメータ

以下はそのまま ACR コマンドに渡す:

| パラメータ | 説明 |
|-----------|------|
| `--base <ref>` | diff 基準 ref（省略時は ACR のデフォルト: `main`、`.acr.yaml` で上書き可） |
| `--reviewer-agent <agents>` | 使用エージェント指定（例: `codex,claude,gemini`） |
| `--verbose` | 詳細出力 |
| `--strict` | advisory もブロッキング扱い |
| その他 | ACR が受け付けるフラグはそのまま渡す |

---

## ACR バイナリ

レビュー実行には安定バイナリのみを使用する。環境変数 `$env:ACR_BIN` が設定されている場合はそのパスを使用し、未設定の場合は以下のデフォルトパスを使用する:

```
C:\Users\kondo\go\bin\acr.exe
```

テストビルド `.\acr.exe` は ACR 自体の開発テスト専用であり、レビューゲートには使用しない。

以降のコマンド例では ACR バイナリパスを `$ACR_BIN` と表記する。実行時は `$env:ACR_BIN` が設定済みならそのパス、未設定ならデフォルトパスを使用すること。

---

## 単発レビューモード（デフォルト）

`--gate` が指定されていない場合、以下の手順で実行する。

### ステップ 1: ACR 実行

```powershell
$ACR_BIN --local --format json --base <base> --verbose [追加パラメータ] 2>.acr/acr_stderr.tmp
```

`--base` が省略された場合は ACR のデフォルト（`main`、`.acr.yaml` で上書き可）に委ねる。

`--verbose` を常に付与し、stderr を一時ファイルに保存する。stderr には `Auto-phase:` 行（ACR の動作モード: small/medium/grouped 等）やモデル構成が出力されるため、レポートで使用する。

### ステップ 2: Exit code チェック

| Exit code | 意味 | アクション |
|-----------|------|-----------|
| 0 | レビュー成功（blocking findings なし） | JSON パースへ進む |
| 1 | レビュー成功（findings あり） | JSON パースへ進む |
| 2 | ACR エラー | stderr を表示して終了。ユーザーに報告する |
| 130 | 中断された | 中断メッセージを表示して終了 |

blocking / advisory / ok の判定は JSON の `verdict` フィールドを参照する。exit code 0/1 の区別だけでは verdict を判断できない。

exit code 2 の場合:
```
## ACR 実行エラー

ACR がエラーで終了しました（exit code: 2）。

**stderr:**
{stderr 内容}

対処: ACR の設定や引数を確認してください。
```

exit code 130 の場合:
```
## ACR 実行中断

ACR が中断されました（Ctrl-C）。

必要に応じて再実行してください。
```

### ステップ 3: JSON パース

ACR の stdout を JSON としてパースする。パースに失敗した場合は「ACR 出力のパースに失敗しました」と raw 出力の先頭 500 文字を表示して終了。

パース成功時、以下のフィールドを抽出:
- `ok`: bool
- `verdict`: "blocking" | "advisory" | "ok"
- `findings[]`: FindingGroup の配列
- `info[]`: 情報的な findings
- `notes_for_next_review`: 次回レビュー用メモ
- `skipped_files[]`: スキップされたファイル
- `cross_check`: クロスチェック結果（存在する場合のみ）

### ステップ 4: レポート生成・表示

「レポートフォーマット」セクションに従ってレポートを生成し、表示する。

---

## レビューゲートモード (`--gate`)

`--gate` が指定された場合、以下の反復ループを実行する。

### 前提条件チェック

ゲートモードはファイル I/O が必須。ファイル書き込みができない環境では「ゲートモードにはファイル書き込みが必要です。単発レビューにフォールバックします」と報告し、単発レビューモードにフォールバックする。

### ゲートループ

```
Iteration N (N = 1 から開始):

  1. guidance-file 準備:
     - ユーザーが --guidance-file を passthrough 指定している場合:
       そちらを優先し、スキル側の自動生成は行わない（ステップ 11 もスキップ）
     - iteration 1（ユーザー指定なし）: スキップ
     - iteration 2+（ユーザー指定なし）: .acr/guidance-notes.tmp が存在すれば
       --guidance-file として使用（前 iteration のステップ 11 で生成済み）

  2. ACR 実行:
     $ACR_BIN --local --format json --base <base> --verbose [--guidance-file ...] [追加パラメータ] 2>.acr/acr_stderr.tmp
     ※ iteration 2+ でのみ --guidance-file を付与（ユーザー指定時は常に付与）
     ※ --verbose を常に付与し stderr をキャプチャ（Auto-phase モード情報の抽出用）

  3. Exit code チェック:
     - 0 or 1 → ステップ 4 へ
     - 2 → 異常停止。「ACR の設定や引数を確認」をユーザーに報告して終了
     - 130 → 異常停止。「ACR が中断されました」を報告して終了

  4. JSON パース:
     - 成功 → ステップ 5 へ
     - 失敗 → 異常停止。ユーザーにパースエラーを報告して終了

  5. Verdict 判定:
     - --strict なし: verdict != "blocking" → ループ終了。最終レポートを表示して完了
     - --strict あり: verdict == "ok" のみループ終了。"advisory" も修正対象として続行
     - 上記以外 → ステップ 6 へ

  6. 状態更新:
     ステップ 2 で解決済みの base ref（以降 `<base>`）をそのまま使用する。
     ハッシュ計算時は git 設定による出力差異を排除するため `--no-color --no-ext-diff` を付与する。
     - `git diff --no-color --no-ext-diff <base>` を 1 回実行し、出力を取得する
     - code_diff_hash を計算: 上記出力全体の SHA256 ハッシュ
     - file_diff_hashes を計算: 上記出力を `^diff --git` 行で分割し、各セクションから
       ファイル名（`diff --git a/<path> b/<path>` の b/ 側）を抽出、セクション内容の
       SHA256 ハッシュを計算して {filename: hash} マップを作成
     - 現在の状態を history に追加し、.acr/gate-state.json に書き込む（code_diff_hash, file_diff_hashes 含む）
     - 停止判定の前に記録することで、振動停止・max-iter 停止時も最終 iteration が残る

  7. 振動検知 (iteration >= 3):
     振動とは修正が A→B→A と元に戻るパターンであり、検出には最低 3 回の
     観測が必要。iteration N のコード状態を iteration N-2 のコード状態と比較する。
     ※ history[N-2] は「iteration 番号が N-2 の履歴エントリ」を指す（0-based 配列
       インデックスではない）。history 配列は iteration 順に追加されるため、
       配列上は history[N-3] の位置になることに注意。

     a) 完全振動: code_diff_hash == history[N-2].code_diff_hash
        → コードが iteration N-2 と完全に同一の状態に戻った → 振動停止

     b) 部分振動: iteration N-2 → N-1 で変更されたファイルが N で元に戻ったか判定
        - modified = {f : f の hash が history[N-2] と history[N-1] で異なる}
          （片方にのみ存在するファイルも含む）
        - reverted = {f in modified : f の hash が history[N] と history[N-2] で一致}
          （N-2 に存在しなかったファイルが N でも存在しない場合も一致とみなす）
        - |modified| == 0 → 部分振動判定をスキップ（N-2 と N-1 間で変化なし）
        - |reverted| / |modified| >= 70% → 振動停止

     振動停止と max-iter 停止が同一 iteration で該当する場合、振動停止を優先する
     （原因の説明としてより正確なため）

  8. Max-iter チェック:
     - iteration >= max_iter → 停止。残存 findings を報告して終了

  9. レポート表示:
     - --strict なし: blocking findings を MD ブロックで表示
       （findings[] + cross_check.findings[] の blocking を含む）
     - --strict あり: blocking + advisory findings を MD ブロックで表示
       （findings[] + cross_check.findings[] の該当分を含む）
     - 修正対象の findings のみ表示する

  10. 修正実行:
      - レポートの findings を元に、コードを修正する
      - 修正はあなた自身（呼び出し元エージェント）が自律的に実施する

  11. guidance notes 生成・保存:
      ユーザーが --guidance-file を passthrough 指定している場合はスキップする。

      以下の内容を .acr/guidance-notes.tmp に書き込む（蓄積せず毎回上書き）:

      a) ACR の notes_for_next_review（空でなければ先頭に配置）
      b) スキル自身が生成する修正サマリ:
         - 今回の iteration で修正対象とした findings のタイトル一覧
         - 各 finding に対して何をどう修正したかの簡潔な説明
         - 意図的に修正しなかった advisory findings があればその理由

      このサマリは次 iteration の ACR reviewer に --guidance-file 経由で渡され、
      前回の修正意図を把握した上でレビューできるようにする。
      これにより、修正済み事項の再指摘（振動）を抑制する。

  12. iteration++ → ステップ 2 へ
```

### 振動停止レポート

```markdown
## ACR Review — 振動検知により停止

反復 {N} 回目で、コード状態が反復 {N-2} 回目と同一（または 70% 以上一致）に戻りました。
修正が A→B→A パターンで振動しており、これ以上の自動修正では収束しない可能性があります。

- **検知種別**: {完全振動 | 部分振動（一致率 XX%）}
- **比較対象**: iteration {N} vs iteration {N-2}

**残存 findings:**
{各 finding の MD ブロック}

手動での対応を検討してください。
```

### Max-iter 停止レポート

```markdown
## ACR Review — 最大反復回数到達

{max_iter} 回の反復で blocking findings が解消されませんでした。

**残存 findings ({count} 件):**
{各 finding の MD ブロック}

手動での対応を検討してください。
```

---

## レポートフォーマット

### 全体構造

```markdown
## ACR Review Report
- **Verdict**: {verdict}
- **Mode**: {auto_phase_mode}
- **Blocking**: {blocking_count} 件
- **Advisory**: {advisory_count} 件
- **Iteration**: {N} / {max_iter}  ← ゲートモード時のみ

---

{各 finding の MD ブロック}

---

## Cross-Check Findings  ← cross_check.findings がある場合のみ
{各 cross-check finding の MD ブロック}

## Info  ← info[] が空でない場合のみ
{各 info の内容}

## Skipped Files  ← skipped_files[] が空でない場合のみ
{各ファイル名を箇条書き}

## Notes for Next Review  ← ゲートモード時のみ
{notes_for_next_review の内容}
```

### Finding ブロック

全 finding（findings[] と cross_check.findings[] の両方）に対して 1 から始まる通し番号 `{seq}` を振る。findings[] を先に番号付けし、cross_check.findings[] はその続番とする。

各 finding について以下のブロックを出力:

```markdown
### #{seq} [{SEVERITY}] {title}
- **Severity**: {severity}
- **Phase**: {phase}

**要約:** {summary}

**詳細:**
{messages[] の各要素を改行区切りで表示}

**修正方針:**
{findings の内容を分析し、具体的な修正方針を自ら策定して記載する}
```

フィールドの導出ルール:
- `{auto_phase_mode}`: stderr の `Auto-phase:` 行から抽出（例: "medium (3 files, 89 lines)"、"grouped (arch+2 diff groups)"）。`Auto-phase:` 行が見つからない場合は "flat (auto-phase off)" と表示
- `{SEVERITY}`: `severity` を大文字化（"BLOCKING" / "ADVISORY"）。空文字の場合は "ADVISORY"
- `{phase}`: `group_key` が "arch" なら "arch"、それ以外なら "diff ({group_key})"。空なら "diff"
- `{title}`: `title` フィールドをそのまま使用
- `{summary}`: `summary` フィールドをそのまま使用
- `{messages[]}`: 各メッセージを改行で区切って表示
- **修正方針**: あなた自身が finding の内容を分析し、どのファイルのどの部分をどう修正すべきか具体的に策定する

### Cross-Check Finding ブロック

Cross-check finding 固有のフィールド導出ルール:
- `{SEVERITY}`: Finding ブロックと同一ルール（`severity` を大文字化。空文字の場合は "ADVISORY"）
- `{type}`: `type` フィールドをそのまま使用（"escalation" / "gap" 等）
- `{involved_groups}`: `involved_groups` 配列をカンマ区切りで結合
- **修正方針**: cross-check の内容と `related_finding_ids` で紐づく arch/diff findings を分析し、修正方針を策定する。対応する arch/diff finding（`#{seq}`）で既に同等の修正方針を述べている場合は番号参照でよい

```markdown
### #{seq} [{SEVERITY}] [CROSS-CHECK] {title}
- **Severity**: {severity}
- **Type**: {type}
- **関連グループ**: {involved_groups をカンマ区切りで表示}

**要約:** {summary}

**修正方針:**
{cross-check の内容と関連する arch/diff findings を分析し、具体的な修正方針を策定する。
 関連する arch/diff finding で既に修正方針を述べている場合は、そちらを参照する形でよい
 （例: "上記 #{seq} の修正方針に準ずる"）}
```

### カウントの計算

- `blocking_count`: `findings[]` のうち `severity == "blocking"` の件数 + `cross_check.findings[]` のうち `severity == "blocking"` の件数
- `advisory_count`: `findings[]` のうち `severity != "blocking"` の件数（空文字含む） + `cross_check.findings[]` のうち `severity != "blocking"` の件数

---

## 状態管理

### 状態ファイル (.acr/gate-state.json)

ゲートモードでのみ使用。以下のスキーマで書き込む:

```json
{
  "pid": 12345,
  "started_at": "2026-05-05T14:00:00+09:00",
  "iteration": 1,
  "max_iter": 3,
  "mode": "gate",
  "base": "HEAD~1",
  "history": [
    {
      "iteration": 1,
      "verdict": "blocking",
      "blocking_count": 2,
      "advisory_count": 1,
      "code_diff_hash": "a1b2c3d4e5f6...",
      "file_diff_hashes": {
        "internal/agent/codex.go": "f1a2b3...",
        "cmd/acr/review.go": "c4d5e6..."
      }
    }
  ]
}
```

### 並行セッション対応

- 新セッション開始時、既存の state ファイルを確認する
- `started_at` が 1 時間以上古い場合は stale として上書きする
- `pid` フィールドが現在のプロセス ID と一致する場合は同一セッションとして続行する
- 上記いずれでもない場合は「別のゲートセッションが実行中の可能性があります」とユーザーに警告する

### Guidance ファイル (.acr/guidance-notes.tmp)

- iteration 2+ で前回の修正コンテキストを書き込む
- ACR の `--guidance-file` フラグで渡す
- 直近 1 iteration の内容のみ使用する（蓄積しない、毎回上書き）
- 内容は以下の 2 パートで構成:
  1. ACR の `notes_for_next_review`（空でなければ先頭に配置）
  2. スキルが生成する修正サマリ（修正した findings、修正内容、未修正の理由）
- 両パートとも空の場合のみ guidance-file を使用しない
- ユーザーが `--guidance-file` を passthrough 指定した場合、スキルの自動生成は行わない（ユーザー指定を優先）

---

## 停止条件まとめ

| 条件 | 判定 | アクション |
|------|------|-----------|
| verdict が通過条件を満たす | 正常終了 | 最終レポート表示。通過条件: --strict なし → verdict != "blocking" / --strict あり → verdict == "ok" |
| ACR exit 2 | 異常停止 | エラー内容を報告してユーザーに設定・引数の確認を促す |
| ACR exit 130 | 中断停止 | 中断された旨を報告し、必要に応じて再実行を促す |
| JSON パース失敗 | 異常停止 | raw 出力を報告してユーザーに判断を仰ぐ |
| max-iter 到達 | 上限停止 | 残存 findings を報告してユーザーに判断を仰ぐ |
| 振動検知 (iteration >= 3) | 収束不能 | コード状態が iteration N-2 と一致（完全一致または per-file 70% 以上一致）。残存 findings を報告してユーザーに判断を仰ぐ |

---

## 正常終了レポート（ゲートモード）

```markdown
## ACR Review — 全 Blocking Findings 解消

{N} 回の反復で全ての blocking findings が解消されました。

- **最終 Verdict**: {verdict}
- **Advisory**: {advisory_count} 件（残存）
- **反復回数**: {N} / {max_iter}

{advisory findings がある場合はブロックで表示}
```

---

## エラーハンドリング

- `.acr/` ディレクトリが存在しない場合は自動作成する
- state ファイルの書き込みに失敗した場合はワーニングを出すが、レビュー自体は続行する
- ACR バイナリが見つからない場合は「ACR バイナリが見つかりません」と報告して終了する
- `--guidance-file` が ACR に未対応の場合はフラグなしで再実行する
