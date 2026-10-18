#!/usr/bin/env python3
"""Run the mock, real-stack gateway, and load generator in separate processes."""
import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import time
import urllib.request


def ready(url, process):
    for _ in range(200):
        if process.poll() is not None:
            raise RuntimeError(f"server exited: {process.returncode}")
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if response.status == 200:
                    return
        except OSError:
            time.sleep(0.05)
    raise RuntimeError(f"server not ready: {url}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--n", type=int, default=20000)
    parser.add_argument("--label", default="baseline")
    parser.add_argument("--profile", action="store_true")
    parser.add_argument("--rates", default="1000,2500,5000,10000")
    parser.add_argument("--closed", default="1,16")
    parser.add_argument("--delays", default="0,5ms")
    parser.add_argument("--batch-delay", default="0")
    parser.add_argument("--foreground", action="store_true", help="clear macOS gateway background policy using taskpolicy")
    args = parser.parse_args()
    root = Path(".cache/proc") / f"{args.label}-{secrets.token_hex(4)}"
    root.mkdir(parents=True)
    results = []
    for delay in args.delays.split(","):
        cases = [("rate", int(r)) for r in args.rates.split(",") if r]
        cases += [("c", int(c)) for c in args.closed.split(",") if c]
        for mode, value in cases:
            name = f"delay{delay}-{mode}{value}"
            directory = root / name
            directory.mkdir()
            processes = []
            files = []
            try:
                for binary, flags, procs in [
                    ("mockupstream", ["-addr", "127.0.0.1:19090", "-delay", delay], "2"),
                    ("procgateway", [
                        "-db", str(directory / "agentgate.db"),
                        "-credentials", str(directory / "key.json"),
                        "-batch-delay", args.batch_delay,
                    ] + (["-profile", str(directory / "gateway")] if args.profile else []), "4"),
                ]:
                    output = open(directory / f"{binary}.log", "w")
                    files.append(output)
                    env = dict(os.environ, GOMAXPROCS=procs)
                    if args.profile and binary == "procgateway":
                        env["GODEBUG"] = "gctrace=1"
                    process = subprocess.Popen([f".cache/proc/bin/{binary}", *flags], env=env,
                                               stdout=output, stderr=output)
                    processes.append(process)
                    if binary == "procgateway" and args.foreground:
                        taskpolicy = shutil.which("taskpolicy")
                        if taskpolicy is None:
                            raise RuntimeError("taskpolicy is unavailable")
                        policy = subprocess.run([taskpolicy, "-B", "-p", str(process.pid)],
                                                text=True, capture_output=True)
                        (directory / "taskpolicy.log").write_text(policy.stdout + policy.stderr)
                        if policy.returncode:
                            raise RuntimeError("taskpolicy failed; see taskpolicy.log")
                    ready("http://127.0.0.1:" + ("19090" if binary == "mockupstream" else "18080") + "/healthz", process)
                command = [".cache/proc/bin/loadgen", "-n", str(args.n),
                           "-credentials", str(directory / "key.json"),
                           "-output", str(directory / "samples.csv"),
                           "-rate", str(value if mode == "rate" else 0), "-c", str(value if mode == "c" else 16)]
                completed = subprocess.run(command, env=dict(os.environ, GOMAXPROCS="2"),
                                           text=True, capture_output=True)
                (directory / "loadgen.log").write_text(completed.stdout + completed.stderr)
                if completed.returncode and not completed.stdout.strip().startswith("{"):
                    print(completed.stdout + completed.stderr, flush=True)
                    raise RuntimeError(f"load generator failed: {directory}")
                result = dict(json.loads(completed.stdout), delay=delay, name=name,
                              functional_pass=completed.returncode == 0)
                results.append(result)
                print(json.dumps(result), flush=True)
                (directory / "summary.json").write_text(json.dumps(result, indent=2) + "\n")
            finally:
                for process in reversed(processes):
                    if process.poll() is None:
                        process.send_signal(signal.SIGTERM)
                        try:
                            process.wait(timeout=30)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait()
                for file in files:
                    file.close()
    (root / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    print(f"Results: {root}", flush=True)
    if any(not result["functional_pass"] for result in results):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
