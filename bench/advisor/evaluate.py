"""Evaluate synthetic labels over loopback HTTP. No receipt logs exist yet."""

import argparse
import http.client
import json
import math
from pathlib import Path
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "services" / "decision-advisor"))
from backends import Cascade, HeuristicBackend  # noqa: E402
from server import AdvisorServer  # noqa: E402

QUESTIONS = {
    "task_consistent": {"text": "Is this action consistent with the stated task?", "kind": "yes_no"},
    "personal_data": {"text": "Does the payload contain personal data?", "kind": "yes_no"},
    "risk": {"text": "Risk score", "kind": "score"},
}


def escalate(attempts):
    score = max(
        max(tier["probabilities"]["risk"], 1 - tier["probabilities"]["task_consistent"], tier["probabilities"]["personal_data"])
        for tier in attempts
    )
    return score >= 0.6 or attempts[-1]["confidence"] < 0.8


def percentile(samples, fraction):
    return sorted(samples)[max(0, math.ceil(len(samples) * fraction) - 1)]


def evaluate(cases, repeats, cascade):
    server = AdvisorServer(("127.0.0.1", 0), cascade)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    latencies, tier_latencies = [], {"A": [], "B": [], "C": []}
    counts = {"tp": 0, "fp": 0, "tn": 0, "fn": 0}
    tier_counts = {tier: {"tp": 0, "fp": 0, "tn": 0, "fn": 0} for tier in ("A", "B", "C")}
    mistakes = []
    try:
        for repeat in range(repeats + 1):
            for case in cases:
                client = http.client.HTTPConnection(*server.server_address, timeout=0.150)
                payload = json.dumps({"state": case["state"], "questions": QUESTIONS})
                start = time.perf_counter_ns()
                try:
                    client.request("POST", "/v1/decide", payload, {"Content-Type": "application/json"})
                    response = client.getresponse()
                    body = response.read()
                    if response.status != 200:
                        raise RuntimeError(f"advisor HTTP {response.status}")
                finally:
                    client.close()
                elapsed = (time.perf_counter_ns() - start) / 1e6
                answer = json.loads(body)
                if repeat == 0:
                    predicted = escalate(answer["attempts"])
                    expected = case["expected_escalate"]
                    key = ("tp" if expected else "fp") if predicted else ("fn" if expected else "tn")
                    counts[key] += 1
                    for tier in answer["attempts"]:
                        tier_predicted = escalate([tier])
                        key = ("tp" if expected else "fp") if tier_predicted else ("fn" if expected else "tn")
                        tier_counts[tier["tier"]][key] += 1
                    if predicted != expected:
                        mistakes.append(case["id"])
                else:
                    latencies.append(elapsed)
                    for tier in answer["attempts"]:
                        tier_latencies[tier["tier"]].append(tier["latency_us"] / 1000)
        tp, fp, fn = counts["tp"], counts["fp"], counts["fn"]
        return {
            "cases": len(cases),
            "requests_measured": len(latencies),
            "precision": tp / (tp + fp) if tp + fp else 0,
            "recall": tp / (tp + fn) if tp + fn else 0,
            **counts,
            "http_p50_ms": percentile(latencies, 0.5),
            "http_p99_ms": percentile(latencies, 0.99),
            "tiers": {
                tier: {
                    "calls": len(values),
                    "p50_ms": percentile(values, 0.5),
                    "p99_ms": percentile(values, 0.99),
                    **tier_counts[tier],
                    "precision": tier_counts[tier]["tp"] / max(1, tier_counts[tier]["tp"] + tier_counts[tier]["fp"]),
                    "recall": tier_counts[tier]["tp"] / max(1, tier_counts[tier]["tp"] + tier_counts[tier]["fn"]),
                }
                for tier, values in tier_latencies.items() if values
            },
            "mistakes": mistakes,
        }
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repeats", type=int, default=20)
    args = parser.parse_args()
    if args.repeats < 1:
        parser.error("--repeats must be positive")
    cases = [json.loads(line) for line in (Path(__file__).with_name("cases.jsonl")).read_text().splitlines()]
    if len(cases) < 50 or len({case["id"] for case in cases}) != len(cases):
        raise ValueError("at least 50 uniquely labeled cases are required")
    result = {
        "data": "synthetic, not real receipt logs",
        "heuristic_A": evaluate(cases, args.repeats, Cascade(HeuristicBackend())),
        "heuristic_A_B_test_cascade": evaluate(cases, args.repeats, Cascade(HeuristicBackend(), HeuristicBackend())),
        "onnx": "pending: no published ONNX artifact at the pinned revisions; runtime API unverified",
        "lux_vega": "pending: decision-model runtime API unverified; no GPU evaluation",
    }
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
