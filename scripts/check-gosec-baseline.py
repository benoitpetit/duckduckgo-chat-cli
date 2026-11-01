#!/usr/bin/env python3
"""Compare a gosec SARIF report with the reviewed repository baseline."""

from collections import Counter
import json
import os
from pathlib import Path
import sys


def read_report(path):
    with open(path, encoding="utf-8") as report_file:
        return json.load(report_file)


def results(report):
    runs = report.get("runs")
    if not isinstance(runs, list) or not runs:
        raise ValueError("SARIF report has no runs")
    all_results = []
    for run in runs:
        run_results = run.get("results", [])
        if not isinstance(run_results, list):
            raise ValueError("SARIF run has an invalid results field")
        all_results.extend(run_results)
    return all_results


def finding_key(result):
    location = result.get("locations", [{}])[0].get("physicalLocation", {})
    artifact = location.get("artifactLocation", {})
    region = location.get("region", {})
    snippet = region.get("snippet", {}).get("text")
    identity = snippet if snippet else f"line:{region.get('startLine', 0)}"
    return (
        result.get("ruleId", ""),
        artifact.get("uri", ""),
        identity,
        result.get("message", {}).get("text", ""),
    )


def main():
    if len(sys.argv) != 3:
        print("usage: check-gosec-baseline.py CURRENT.sarif BASELINE.sarif", file=sys.stderr)
        return 2

    current_report = read_report(sys.argv[1])
    baseline_report = read_report(sys.argv[2])
    current_results = results(current_report)
    baseline_results = results(baseline_report)
    current_counts = Counter(finding_key(item) for item in current_results)
    baseline_counts = Counter(finding_key(item) for item in baseline_results)
    new_counts = current_counts - baseline_counts
    resolved_counts = baseline_counts - current_counts
    matched_count = sum((current_counts & baseline_counts).values())

    print(
        "gosec baseline: "
        f"{len(baseline_results)} findings; current: {len(current_results)}; "
        f"existing: {matched_count}; new: {sum(new_counts.values())}; "
        f"resolved: {sum(resolved_counts.values())}"
    )

    if new_counts:
        print("New findings:")
        for result in current_results:
            key = finding_key(result)
            if new_counts[key] <= 0:
                continue
            new_counts[key] -= 1
            location = result.get("locations", [{}])[0].get("physicalLocation", {})
            path = location.get("artifactLocation", {}).get("uri", "<unknown>")
            line = location.get("region", {}).get("startLine", 0)
            print(f"  {result.get('ruleId', '?')} {path}:{line}: {result.get('message', {}).get('text', '')}")

    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary_path:
        summary = [
            "### gosec findings",
            "",
            "| Baseline | Current | Existing | New | Resolved |",
            "| ---: | ---: | ---: | ---: | ---: |",
            f"| {len(baseline_results)} | {len(current_results)} | {matched_count} | {sum((current_counts - baseline_counts).values())} | {sum(resolved_counts.values())} |",
            "",
            "New findings fail this check and require triage. The SARIF report is uploaded separately, including existing findings.",
        ]
        with open(summary_path, "a", encoding="utf-8") as summary_file:
            summary_file.write("\n".join(summary) + "\n")

    return 1 if new_counts else 0


if __name__ == "__main__":
    sys.exit(main())
