#!/usr/bin/env python3
"""Summarize correlated tail samples and optional Go parsed trace regions."""
import argparse
import collections
import csv
import json
import math
import re


def quantiles(values):
    values = sorted(values)
    return {name: values[math.ceil(len(values) * fraction) - 1] / 1e6
            for name, fraction in [("p50_ms", .5), ("p99_ms", .99), ("max_ms", 1)]}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("samples")
    parser.add_argument("--trace", help="go tool trace -d=parsed output")
    args = parser.parse_args()
    with open(args.samples) as file:
        rows = [{key: int(value) for key, value in row.items() if key.endswith("_ns")}
                for row in csv.DictReader(file)]
    ranked = sorted(range(len(rows)), key=lambda i: rows[i]["total_ns"] - rows[i]["upstream_ns"])
    if not rows:
        raise ValueError("no samples")
    tail = ranked[min(len(rows) - 1, math.ceil(.99 * len(rows))):]
    totals = collections.defaultdict(int)
    for index in tail:
        row = rows[index]
        totals["dispatch"] += row["dispatch_ns"]
        totals["auth"] += row["auth_ns"]
        totals["receipt"] += row["receipt_ns"]
        totals["other_gateway"] += row["handler_ns"] - row["auth_ns"] - row["receipt_ns"] - row["upstream_ns"]
        totals["transport_and_response"] += row["total_ns"] - row["dispatch_ns"] - row["handler_ns"]
    result = {
        "tail_count": len(tail),
        "tail_mean_ms": sum(totals.values()) / len(tail) / 1e6,
        "tail_components": {key: {"mean_ms": value / len(tail) / 1e6,
                                  "share_pct": value / sum(totals.values()) * 100}
                            for key, value in totals.items()},
    }
    if args.trace:
        starts, active, latest, tasks, appends, replies = {}, {}, {}, {}, {}, {}
        durations = collections.defaultdict(list)
        pauses = []
        pause_start = None
        with open(args.trace) as file:
            for line in file:
                region = re.search(r'G=(-?\d+) (RegionBegin|RegionEnd) Time=(\d+) Task=(\d+) Type="([^"]+)"', line)
                if region:
                    goroutine, event, timestamp, task, name = region.groups()
                    timestamp = int(timestamp)
                    key = (goroutine, task, name)
                    if event == "RegionBegin":
                        starts[key] = timestamp
                        if name == "receipt.transaction":
                            active[goroutine] = {"start": timestamp, "phases": collections.defaultdict(int)}
                    elif key in starts:
                        start = starts.pop(key)
                        durations[name].append(timestamp - start)
                        if name == "receipt.transaction":
                            transaction = active.pop(goroutine)
                            transaction["end"] = timestamp
                            latest[goroutine] = transaction
                        elif name == "receipt.append":
                            appends[task] = (start, timestamp)
                        elif name.startswith("receipt.") and goroutine in active:
                            active[goroutine]["phases"][name] += timestamp - start
                message = re.search(r'G=(-?\d+) Log Time=\d+ Task=(\d+) Category="([^"]+)" Message="([^"]*)"', line)
                if message:
                    goroutine, task, category, value = message.groups()
                    if category == "request.id" and value and int(value) >= 0:
                        tasks[int(value)] = task
                    if category == "receipt.reply" and goroutine in latest:
                        replies[task] = latest[goroutine]
                pause = re.search(r'(RangeBegin|RangeEnd) Time=(\d+) Name="stop-the-world \(GC [^"]+"', line)
                if pause:
                    event, timestamp = pause.groups()
                    if event == "RangeBegin":
                        pause_start = int(timestamp)
                    elif pause_start is not None:
                        pauses.append((pause_start, int(timestamp)))
                        pause_start = None
        result["regions"] = {name: dict(quantiles(values), count=len(values)) for name, values in durations.items()}
        result["gc_stw"] = dict(quantiles([end - start for start, end in pauses]), count=len(pauses)) if pauses else {}
        phases = collections.defaultdict(int)
        matched = 0
        for index in tail:
            task = tasks.get(index)
            if task not in appends or task not in replies:
                continue
            start, end = appends[task]
            transaction = replies[task]
            matched += 1
            phases["queue_and_writer_acquire"] += transaction["start"] - start
            phases["ack_schedule"] += end - transaction["end"]
            known = 0
            for name, duration in transaction["phases"].items():
                phases[name] += duration
                known += duration
            phases["transaction_other"] += transaction["end"] - transaction["start"] - known
            phases["overlapping_gc_stw"] += sum(max(0, min(end, pe) - max(start, ps)) for ps, pe in pauses)
        result["tail_receipt_trace"] = {
            "matched": matched,
            "mean_ms": {name: value / matched / 1e6 for name, value in phases.items()} if matched else {},
        }
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
