"""ACR Role-Prompts Benchmark Analyzer."""
import json
import glob
import sys
from pathlib import Path
from statistics import mean, stdev

OUTDIR = Path(__file__).parent


def load_trial(path: Path) -> dict | None:
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, FileNotFoundError):
        return None


def extract_metrics(data: dict) -> dict:
    findings = data.get("findings", [])
    info = data.get("info", [])
    cc = data.get("cross_check", {})
    cc_findings = cc.get("findings", []) if cc else []

    blocking = [f for f in findings if f.get("severity") == "blocking"]
    advisory = [f for f in findings if f.get("severity") != "blocking"]
    dup = [f for f in findings if f.get("reviewer_count", 0) >= 2]
    arch_src = [f for f in findings if f.get("arch_reviewer_count", 0) > 0]
    diff_src = [f for f in findings if f.get("diff_reviewer_count", 0) > 0]

    return {
        "findings": len(findings),
        "blocking": len(blocking),
        "advisory": len(advisory),
        "info": len(info),
        "verdict": data.get("verdict", "?"),
        "dup": len(dup),
        "arch_src": len(arch_src),
        "diff_src": len(diff_src),
        "cc_findings": len(cc_findings),
    }


def summarize(metrics_list: list[dict]) -> None:
    n = len(metrics_list)
    if n == 0:
        return
    for key in ["findings", "blocking", "advisory", "info", "dup", "arch_src", "diff_src", "cc_findings"]:
        vals = [m[key] for m in metrics_list]
        avg = mean(vals)
        sd = stdev(vals) if n > 1 else 0
        print(f"  {key:12s}: mean={avg:.1f}  sd={sd:.1f}  range=[{min(vals)}, {max(vals)}]")


def main():
    for condition in ["no-role-prompts", "role-prompts"]:
        print(f"\n=== {condition} ===")
        trials = sorted(OUTDIR.glob(f"{condition}_trial*.json"))
        if not trials:
            print("  (no data)")
            continue

        metrics_list = []
        for path in trials:
            data = load_trial(path)
            if data is None:
                print(f"  {path.name}: FAILED TO PARSE")
                continue
            m = extract_metrics(data)
            trial_id = path.stem.split("trial")[-1]
            print(
                f"  Trial {trial_id}: findings={m['findings']} "
                f"(blk={m['blocking']}, adv={m['advisory']}) "
                f"info={m['info']} verdict={m['verdict']} "
                f"dup={m['dup']} arch={m['arch_src']} diff={m['diff_src']} "
                f"cc={m['cc_findings']}"
            )
            metrics_list.append(m)

        print("  ----")
        summarize(metrics_list)

    # Timing
    timing_path = OUTDIR / "timing.csv"
    if timing_path.exists():
        print("\n=== Timing ===")
        lines = timing_path.read_text().strip().split("\n")
        for line in lines:
            print(f"  {line}")

        # Per-condition timing summary
        for condition in ["no-role-prompts", "role-prompts"]:
            durations = []
            for line in lines[1:]:
                parts = line.split(",")
                if len(parts) == 4 and parts[0] == condition:
                    durations.append(int(parts[3]))
            if durations:
                avg = mean(durations)
                print(f"  {condition} avg duration: {avg:.0f}s")


if __name__ == "__main__":
    main()
