# ARC Review Workflow

コードを変更した後、品質ゲートとして ARC レビューの実行を検討すること。

- 重要な変更やレビュー前の最終確認には `/arc-review` を使用する
- 反復的な修正が必要な場合は `/arc-review --gate` を使用する
- ARC の動作設定（agents, models, phases）は `.arc.yaml` で管理する
- レビュー実行には安定バイナリを使用する（`$env:ARC_BIN` または `C:\Users\kondo\go\bin\arc.exe`）
