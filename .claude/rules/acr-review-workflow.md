# ACR Review Workflow

コードを変更した後、品質ゲートとして ACR レビューの実行を検討すること。

- 重要な変更やレビュー前の最終確認には `/acr-review` を使用する
- 反復的な修正が必要な場合は `/acr-review --gate` を使用する
- ACR の動作設定（agents, models, phases）は `.acr.yaml` で管理する
- レビュー実行には安定バイナリを使用する（`$env:ACR_BIN` または `C:\Users\kondo\go\bin\acr.exe`）
