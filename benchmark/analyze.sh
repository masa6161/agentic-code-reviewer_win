#!/bin/bash
# ARC Benchmark Result Analyzer
# Extracts metrics from JSON output files

OUTDIR="benchmark"

echo "=== ARC Role-Prompts Benchmark Results ==="
echo ""

for condition in no-role-prompts role-prompts; do
    echo "--- Condition: ${condition} ---"
    total_findings=0
    total_blocking=0
    total_advisory=0
    total_info=0
    total_cc=0
    trial_count=0

    for f in "${OUTDIR}/${condition}_trial"*.json; do
        [ -f "$f" ] || continue
        trial_count=$((trial_count + 1))
        trial=$(basename "$f" | sed 's/.*trial\([0-9]*\).*/\1/')

        findings=$(jq '.findings | length' "$f" 2>/dev/null || echo 0)
        blocking=$(jq '[.findings[] | select(.severity == "blocking")] | length' "$f" 2>/dev/null || echo 0)
        advisory=$(jq '[.findings[] | select(.severity != "blocking")] | length' "$f" 2>/dev/null || echo 0)
        info=$(jq '.info | length' "$f" 2>/dev/null || echo 0)
        verdict=$(jq -r '.verdict' "$f" 2>/dev/null || echo "?")
        dup=$(jq '[.findings[] | select(.reviewer_count >= 2)] | length' "$f" 2>/dev/null || echo 0)
        arch_src=$(jq '[.findings[] | select(.arch_reviewer_count > 0)] | length' "$f" 2>/dev/null || echo 0)
        diff_src=$(jq '[.findings[] | select(.diff_reviewer_count > 0)] | length' "$f" 2>/dev/null || echo 0)
        cc_findings=$(jq 'if .cross_check then .cross_check.findings | length else 0 end' "$f" 2>/dev/null || echo 0)

        echo "  Trial ${trial}: findings=${findings} (blocking=${blocking}, advisory=${advisory}) info=${info} verdict=${verdict} dup=${dup} arch=${arch_src} diff=${diff_src} cc=${cc_findings}"

        total_findings=$((total_findings + findings))
        total_blocking=$((total_blocking + blocking))
        total_advisory=$((total_advisory + advisory))
        total_info=$((total_info + info))
        total_cc=$((total_cc + cc_findings))
    done

    if [ "$trial_count" -gt 0 ]; then
        avg_findings=$(echo "scale=1; $total_findings / $trial_count" | bc)
        avg_blocking=$(echo "scale=1; $total_blocking / $trial_count" | bc)
        avg_advisory=$(echo "scale=1; $total_advisory / $trial_count" | bc)
        avg_info=$(echo "scale=1; $total_info / $trial_count" | bc)
        avg_cc=$(echo "scale=1; $total_cc / $trial_count" | bc)
        echo "  ----"
        echo "  Avg: findings=${avg_findings} blocking=${avg_blocking} advisory=${avg_advisory} info=${avg_info} cc=${avg_cc} (n=${trial_count})"
    fi
    echo ""
done

echo "--- Timing ---"
if [ -f "${OUTDIR}/timing.csv" ]; then
    cat "${OUTDIR}/timing.csv"
fi
