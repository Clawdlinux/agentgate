# Local act load test

Run only against your local gateway. These limits are unsafe for production.
The script tests 1 service, 1 HMAC agent key, and 1 cached bearer token.
Every successful action still commits a signed receipt.

## Start the mock upstream

Run commands from the repository root. Keep each server in its own terminal.
Keep caches and scratch data inside the worktree:

```sh
mkdir -p .cache/tmp .cache/go-tmp .cache/go-build
export TMPDIR="$PWD/.cache/tmp"
export GOTMPDIR="$PWD/.cache/go-tmp"
export GOCACHE="$PWD/.cache/go-build"
export GOMAXPROCS=4
go run ./bench/mockupstream -addr 127.0.0.1:9090 -delay 0
```

Check readiness from another terminal:

```sh
curl --fail http://127.0.0.1:9090/healthz
```

Restart with `-delay 5ms` for the second scenario.
The mock returns measured handler nanoseconds inside its JSON body.
The gateway wraps this body without changing those measurements.
Sleep overshoot is measured rather than assumed.

## Start the gateway

Set the same cache variables in this terminal.
Generate local secrets. Don't commit them.

```sh
export AGENTGATE_VAULT_KEY="$(openssl rand -hex 16)"
export AGENTGATE_ADMIN_SECRET="$(openssl rand -hex 24)"
export AGENTGATE_RATE_KEY_RPS=1000000
export AGENTGATE_RATE_KEY_BURST=1000000
export AGENTGATE_RATE_TENANT_RPS=1000000
export AGENTGATE_RATE_TENANT_BURST=1000000
export AGENTGATE_TENANT_MAX_INFLIGHT=1024
GOMAXPROCS=4 go run ./cmd/agentgw \
  --config bench/services.yaml --db .cache/k6-agentgate.db --addr 127.0.0.1:8080
```

Save `AGENTGATE_ADMIN_SECRET` for the next terminal.
The first startup prints a bootstrap agent key. You can ignore it and create a scoped benchmark key.
Keep logs private. The startup message contains that bootstrap credential.
SQLite uses real files, migrations, and the default receipt group-commit settings.
The gateway's upstream pool keeps its production defaults.
Info logging remains enabled. Your terminal or log sink can affect these results.

## Provision credentials

Export the same `AGENTGATE_ADMIN_SECRET` in this terminal.
The following commands need `curl`, `openssl`, and `python3`.

```sh
export MOCK_TOKEN="$(openssl rand -hex 16)"
curl --fail-with-body -sS http://127.0.0.1:8080/admin/tokens \
  -H "X-Admin-Secret: $AGENTGATE_ADMIN_SECRET" \
  -H 'Content-Type: application/json' \
  -d "{\"user_id\":\"bench-user\",\"service\":\"mock\",\"access_token\":\"$MOCK_TOKEN\"}"

export AGENTGATE_AGENT_KEY="$(
  curl --fail-with-body -sS http://127.0.0.1:8080/admin/keys \
    -H "X-Admin-Secret: $AGENTGATE_ADMIN_SECRET" \
    -H 'Content-Type: application/json' \
    -d '{"name":"bench","allowed_services":["mock"],"allowed_users":["bench-user"]}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["key"])'
)"
```

## Run k6

```sh
which k6
for vus in 1 16 64; do
  VUS="$vus" DURATION=60s k6 run \
    --summary-export ".cache/k6-delay0-c${vus}.json" bench/k6/act.js
done
```

Restart the mock with `-delay 5ms`. Repeat with summary filenames containing `delay5ms`.
The `<1 ms p99` threshold returns a nonzero exit status when missed.
The script also fails on unsuccessful requests or missing timing samples.
No deliberate client sleep occurs between requests. This is closed-loop concurrency, not fixed arrival rate.
Use 20,000 or more successful requests per scenario before interpreting p99.9.

`gateway_overhead_ms` subtracts each measured upstream duration before aggregating percentiles.
It uses k6 `http_req_duration`: request sending, response waiting, and response receiving.
Connection setup and blocked time are separate k6 metrics. They aren't included in this estimate.
The mock timestamp excludes response encoding and writing. Those costs stay in the overhead estimate.
These are conservative estimates, not exact replicas of the Go benchmark's handler-completion timestamps.
The setup request validates credentials. Initial per-VU connections remain in the measured k6 run.
Keep the load generator separate when benchmarking an actual 4-vCPU server.

## Validate without k6

Don't install k6 just to validate syntax.
Node can parse the module without resolving k6 imports:

```sh
node --input-type=module --check < bench/k6/act.js
```

This checks syntax only. It doesn't execute k6 imports, scenarios, or thresholds.
See [the recorded benchmark](../RESULTS.md) for measured Go results and the k6 status.
