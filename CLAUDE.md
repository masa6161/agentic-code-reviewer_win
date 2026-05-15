# CLAUDE.md

Claude Code 固有の補足ガイドです。
共通ルールは [AGENTS.md](AGENTS.md)、プロジェクト固有の設計方針や手順は [docs/development.md](docs/development.md) を参照してください。

## 言語

応答、コミットメッセージ、PR 本文には日本語を使用すること。

## アーキテクチャ（詳細ファイル一覧）

以下はナビゲーション用の詳細ファイル一覧です。設計方針やコードパターンは AGENTS.md を参照してください。

```
cmd/arc/                   # CLI エントリポイントとサブコマンド
  main.go                  # CLI エントリポイント、フラグ解析、cobra ルートコマンド
  review.go                # コアレビューオーケストレーション（executeReview）
  review_opts.go           # ReviewOpts 構造体（解決済み設定 + CLI フラグのバンドル）
  pr_submit.go             # PR 投稿フロー（コメント投稿、承認）
  config_cmd.go            # `arc config` サブコマンド（.arc.yaml の初期化/表示）
  help.go                  # カスタムヘルプフォーマット（フラググループ）
  helpers.go               # CLI ヘルパー関数（finding フィルタリング等）
  version.go               # バージョン情報（ldflags 経由で注入）
  signals_unix.go          # Unix シグナル処理（ビルドタグ付き）
  signals_windows.go       # Windows シグナル処理（ビルドタグ付き）
  *_test.go                # review, helpers, config_cmd, pr_submit, help のテスト

internal/
  agent/                   # LLM エージェント抽象化レイヤー
    doc.go                 # パッケージドキュメント
    agent.go               # Agent インターフェース（ExecuteReview, ExecuteSummary）
    codex.go               # Codex CLI エージェント実装
    claude.go              # Claude CLI エージェント実装
    gemini.go              # Gemini CLI エージェント実装
    factory.go             # エージェントおよびパーサーのファクトリ関数（レジストリ）
    cohort.go              # マルチエージェント名解析、可用性チェック、分配
    config.go              # ReviewConfig と SummaryConfig 構造体
    executor.go            # サブプロセス実行（stderr キャプチャ付き）
    cmd_reader.go          # io.Reader ラッパー（プロセスライフサイクル管理付き）
    result.go              # ExecutionResult（io.ReadCloser + 終了コード + stderr）
    auth.go                # 認証失敗検出（終了コード、stderr パターン）
    diff.go                # Git diff/fetch 委譲（後方互換エイリアス）
    diff_review.go         # 差分ベースのレビュー実行（Claude/Gemini 共通）
    parser.go              # ReviewParser と SummaryParser インターフェース
    claude_review_parser.go    # Claude レビュー出力パーサー
    codex_review_parser.go     # Codex レビュー出力パーサー（JSONL）
    gemini_review_parser.go    # Gemini レビュー出力パーサー
    claude_summary_parser.go   # Claude サマリー出力パーサー
    codex_summary_parser.go    # Codex サマリー出力パーサー
    gemini_summary_parser.go   # Gemini サマリー出力パーサー
    prompts.go             # エージェントごとのデフォルトレビュー/サマリープロンプト
    nonfinding.go          # 「問題なし」応答の検出
    reffile.go             # 大規模 diff 用一時ファイル管理
    severity.go            # finding テキストからの重要度抽出
    process_unix.go        # Unix プロセスグループ処理（ビルドタグ付き）
    process_windows.go     # Windows プロセスグループ処理（ビルドタグ付き）
    *_test.go              # 包括的テストスイート

  config/                  # 設定ファイルサポート
    config.go              # .arc.yaml の読み込み/解析、LoadEnvState()、Resolve() 優先順位

  domain/                  # コア型: Finding, AggregatedFinding, GroupedFindings
    finding.go             # Finding 型、集約ロジック、disposition 追跡
    result.go              # ReviewerResult と ReviewStats
    exitcode.go            # 終了コード定数（0=問題なし, 1=指摘あり, 2=エラー, 130=中断）
    phase.go               # フェーズ定数（PhaseArch, PhaseDiff）

  filter/                  # Finding フィルタリング
    filter.go              # 正規表現パターンマッチングによる finding 除外

  fpfilter/                # 誤検出フィルタリングと重要度トリアージ
    filter.go              # LLM ベースの誤検出検出と重要度トリアージ（blocking/advisory/noise）
    prompt.go              # FP フィルターおよびトリアージプロンプトテンプレート

  feedback/                # PR フィードバック要約
    fetch.go               # gh CLI 経由で PR 説明とコメントを取得
    summarizer.go          # LLM ベースの PR ディスカッション要約
    prompt.go              # フィードバック要約プロンプトテンプレート

  runner/                  # レビュー実行エンジン
    runner.go              # 並列レビュワーオーケストレーション
    report.go              # レポートレンダリング（ターミナル + markdown）
    phase.go               # PhaseConfig とマルチフェーズレビュー計画
    spec.go                # ReviewerSpec と分配フォーマット

  summarizer/              # LLM ベースの finding 要約
    summarizer.go          # エージェント実行と出力解析のオーケストレーション
    crosscheck.go          # サマライザー出力のクロスチェック検証

  github/                  # gh CLI 経由の GitHub PR 操作
    pr.go                  # コメント投稿、PR 承認、CI ステータス確認
    fork.go                # フォーク参照解決

  git/                     # Git 操作
    worktree.go            # 一時ワークツリー管理
    diff.go                # diff 生成、ブランチ更新、diff サイズ分類
    diffsplit.go           # unified diff のファイル別分割
    remote.go              # リモート管理（追加、fetch、URL 操作）

  terminal/                # ターミナル UI
    spinner.go             # プログレススピナー
    logger.go              # スタイル付きロギング
    colors.go              # ANSI カラーコード
    format.go              # テキストフォーマットユーティリティ
    selector.go            # インタラクティブ TUI セレクター（bubbletea ベース）

  modelconfig/             # モデル設定解決
    resolver.go            # (サイズ, 役割, エージェント) タプルに対するモデル + effort の解決
```

## フォーク元ドキュメント

フォーク元プロジェクト（Rich Haase 版）の開発ガイドは [CLAUDE_upstream.md](CLAUDE_upstream.md) として保存しています。
