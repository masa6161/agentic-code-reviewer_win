#!/bin/bash
# ACR Role-Prompts A/B Benchmark
# Design: paired comparison on single Large diff
# Base: 0469d70 (pre-role-prompt-tuning merge)
# Diff: 12 files, 417 lines → auto-phase Large
# Conditions: --no-role-prompts (baseline) vs --role-prompts (treatment)
# Trials: 6 per condition, interleaved to reduce temporal bias

BASE="0469d70"
OUTDIR="benchmark"
BINARY="./acr.exe"

mkdir -p "$OUTDIR"

run_trial() {
    local condition=$1
    local trial=$2
    local flag="--${condition}"
    local prefix="${OUTDIR}/${condition}_trial${trial}"

    echo "[$(date '+%H:%M:%S')] Starting ${condition} trial ${trial}..."

    start=$(date +%s)
    ${BINARY} --local --base ${BASE} --verbose ${flag} --format json \
        > "${prefix}.json" \
        2> "${prefix}.log"
    rc=$?
    end=$(date +%s)
    elapsed=$((end - start))

    echo "[$(date '+%H:%M:%S')] ${condition} trial ${trial} done (exit=${rc}, ${elapsed}s)"
    echo "${condition},${trial},${rc},${elapsed}" >> "${OUTDIR}/timing.csv"
}

echo "condition,trial,exit_code,duration_sec" > "${OUTDIR}/timing.csv"

for trial in 1 2 3 4 5 6; do
    run_trial "no-role-prompts" "$trial"
    run_trial "role-prompts" "$trial"
done

echo "=== Benchmark complete ==="
cat "${OUTDIR}/timing.csv"
