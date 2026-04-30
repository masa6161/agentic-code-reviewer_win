# ACR Role-Prompts Benchmark: 定量結果

**実施日**: 2026-04-29
**対象 diff**: 0469d70..HEAD (12 files, 417 lines, auto-phase=Large)
**構成**: arch×1(claude-opus-4-7) + diff×3(codex/gpt-5.5) + cross-check(codex+claude+gemini) + summarizer(claude-opus-4-7)
**設計**: paired comparison, 6 trials × 2 conditions = 12 runs, interleaved

## 定量サマリー

| 指標 | Baseline (no-role-prompts) | Treatment (role-prompts) | 変化 |
|------|---------------------------|-------------------------|------|
| findings | 3.3 (sd=1.0, [2,5]) | 3.3 (sd=0.5, [3,4]) | ±0% (分散半減) |
| blocking | 2.8 (sd=0.8, [2,4]) | 2.5 (sd=0.5, [2,3]) | -11% |
| advisory | 0.5 (sd=0.5, [0,1]) | 0.8 (sd=0.8, [0,2]) | +60% |
| info | 1.0 (sd=0.0, [1,1]) | 3.0 (sd=2.2, [1,7]) | +200% |
| dup (reviewer_count≥2) | 2.0 (sd=0.9, [1,3]) | 1.8 (sd=1.0, [1,3]) | -10% |
| arch_src | 3.3 (sd=1.0, [2,5]) | 3.2 (sd=0.4, [3,4]) | -3% |
| diff_src | 2.0 (sd=0.9, [1,3]) | 2.0 (sd=0.9, [1,3]) | ±0% |
| cc_findings | 10.2 (sd=3.3, [4,13]) | 10.7 (sd=2.5, [6,13]) | +5% |
| avg duration | 439s | 404s | -8% |

## Trial 別データ

### Baseline (no-role-prompts)

| Trial | findings | blk | adv | info | dup | arch | diff | cc | duration |
|-------|----------|-----|-----|------|-----|------|------|----|----------|
| 1 | 2 | 2 | 0 | 1 | 2 | 2 | 2 | 9 | 352s |
| 2 | 3 | 2 | 1 | 1 | 3 | 3 | 3 | 13 | 412s |
| 3 | 3 | 3 | 0 | 1 | 2 | 3 | 2 | 12 | 432s |
| 4 | 3 | 3 | 0 | 1 | 1 | 3 | 1 | 12 | 483s |
| 5 | 4 | 3 | 1 | 1 | 1 | 4 | 1 | 11 | 546s |
| 6 | 5 | 4 | 1 | 1 | 3 | 5 | 3 | 4 | 407s |

### Treatment (role-prompts)

| Trial | findings | blk | adv | info | dup | arch | diff | cc | duration |
|-------|----------|-----|-----|------|-----|------|------|----|----------|
| 1 | 4 | 3 | 1 | 3 | 3 | 4 | 3 | 13 | 417s |
| 2 | 3 | 2 | 1 | 3 | 1 | 3 | 1 | 12 | 452s |
| 3 | 3 | 2 | 1 | 1 | 1 | 3 | 1 | 11 | 366s |
| 4 | 4 | 2 | 2 | 1 | 1 | 3 | 2 | 12 | 366s |
| 5 | 3 | 3 | 0 | 7 | 3 | 3 | 3 | 6 | 286s |
| 6 | 3 | 3 | 0 | 3 | 2 | 3 | 2 | 10 | 534s |

## GO/NO-GO 判定基準

- findings ±20%: **GO** (±0%)
- 定性評価: (別ファイルに記録)
