# Local decision advisor

This separate service exposes `POST /v1/decide`, `GET /healthz`, and `GET /metrics`.
The gateway never imports Python or loads model weights.
The gateway remains the deterministic enforcer.

Only the deterministic `heuristic` backend runs today.
It is a test fixture, not a model-quality claim.
`onnx` and `vllm-sr` are explicit blocked adapters.
See [RUNTIME.md](RUNTIME.md) before attempting model deployment.

## Run and test

From the repository root:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 services/decision-advisor/server.py --port 8081
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s services/decision-advisor -v
PYTHONDONTWRITEBYTECODE=1 python3 bench/advisor/evaluate.py --repeats 20
```

The server binds loopback by default.
It starts each configured backend once.
Tier A can cascade to B, then optional C, below the confidence threshold.
All tiers share a 150 ms budget.
The gateway independently enforces its own 150 ms call deadline.
Model runtimes must support bounded inference before their adapters can ship.

The request limit is 1 MiB.
Requests require one `Content-Length`, JSON media type, and the fixed question set.
Chunked bodies, duplicate JSON keys, and non-finite JSON constants are rejected.
The server caps active connections at 8 and times out socket reads after 150 ms.
Metrics contain counters only, never payloads.

## API

```json
{
  "state": {
    "service": "github",
    "action": "create_issue",
    "params": {"title": "Login fails"}
  },
  "questions": {
    "task_consistent": {"text": "Is this action consistent with the stated task?", "kind": "yes_no"},
    "personal_data": {"text": "Does the payload contain personal data?", "kind": "yes_no"},
    "risk": {"text": "Risk score", "kind": "score"}
  }
}
```

The response contains ordered `attempts` and the applied `cascade_threshold`.
Each attempt reports its tier, model ID, revision, weights digest, probabilities, confidence, and microsecond latency.
For the heuristic, the revision is the source file's SHA-256.
Its weights digest identifies the fixed no-weights ruleset marker.
Those fields do not claim a trained model exists.

The gateway supplies only service, action, and params.
There is no separate task signal in this phase.
The heuristic ignores both `params.stated_task` and top-level task claims.
Neither can raise confidence or establish consistency.
It reports unknown task consistency and low confidence.
All heuristic requests require approval under default gateway thresholds.
The synthetic harness uses this same state shape.

Gateway advisor JSON disables HTML escaping.
Params exceeding 256 KiB become a bounded UTF-8 prefix for the advisor only.
The evidence records `truncated: true` and both original and transmitted state digests.
Truncation and input rejection require approval even under `allow_with_flag`.
The gateway's upstream params and action receipt params digest never change.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `ADVISOR_BACKEND` | `heuristic` | Tier A backend |
| `ADVISOR_TIER_B_BACKEND` | unset | Optional B backend |
| `ADVISOR_TIER_C_BACKEND` | unset | Optional C backend |
| `ADVISOR_CONFIDENCE_THRESHOLD` | `0.8` | Cascade when confidence is lower |
| `ADVISOR_TIER_A_MODEL_DIR` | unset | Read-only local model directory |
| `ADVISOR_TIER_A_REVISION` | unset | Exact 40-character model revision |
| `ADVISOR_TIER_B_MODEL_DIR` / `ADVISOR_TIER_B_REVISION` | unset | B model pins |
| `ADVISOR_TIER_C_MODEL_DIR` / `ADVISOR_TIER_C_REVISION` | unset | C model pins |

Keep the service and gateway confidence thresholds aligned.
Both thresholds are recorded when a valid answer returns.

## Isolation and deployment

Build from this directory. The image uses a digest-pinned Python base and no third-party dependencies.
Run as UID 65532, with a read-only root filesystem and read-only `/models`.
For standalone container checks, pass `--network none` and `--read-only`.
Container images cannot enforce network policy themselves.
The deployment must enforce zero egress.

The [Helm fragments](deploy/) default to disabled.
Tier A uses a CPU DaemonSet in separate pods.
Its service routes to node-local endpoints.
The NetworkPolicy denies all egress, including DNS.
It admits only gateway ingress on port 8081.
Adjust the gateway pod selector for your chart.
Use a CNI that enforces NetworkPolicy.

We avoid sidecars because pod-level policies cannot isolate their egress from a gateway's SaaS traffic.
Ensure each gateway node has a ready advisor endpoint.
Use mirrored images and a preloaded, read-only model PVC.
The current resource values cover the heuristic only.
Real model memory limits require measurement.

Tier B includes a GPU scheduling fragment with a dedicated node selector.
It intentionally starts a blocked adapter.
It exposes no claimed inference protocol and denies all network traffic.
Remote GPU inference wiring remains pending upstream API verification.

## Threat notes

State, params, and task text can contain prompt injection.
Never treat them as instructions to the gateway.
The advisor cannot grant scopes, retrieve credentials, or undo a deterministic denial.
It can only leave an allow unchanged or escalate it.
Earlier high-risk tier observations cannot be erased by later tiers.

The default unavailable policy allows timeouts, connection failures, and service 5xx, with an unavailable flag.
Service 4xx, invalid responses, and local input failures are `advisor_input_rejected`.
They cannot use the allow fallback.
Choose `require_approval` or `deny` if that fallback is inappropriate.
A malicious advisor can cause denials or recommend unsafe allows.
Deterministic policy still bounds every action.
Signed evidence binds what the gateway observed, not the truth of model claims.
Model identity and weight integrity require trusted mirrored artifacts and deployment controls.

HTTP transport ignores environment proxies and rejects redirects.
Gateway configuration accepts only loopback or `.svc.cluster.local` URLs.
The advisor has no outbound HTTP client or downloader.
Retain evidence records with receipt exports.
