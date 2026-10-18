# Advisor evaluation

All 60 cases are synthetic. No real receipt logs exist yet.
Cases contain a state, action, and expected escalation label.
Labels cover writes, payments, outbound messages, and data exposure.
The 6 `edge-*` cases expose known heuristic mistakes.
These labels are not held-out model evaluation data.

Run from the repository root:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 bench/advisor/evaluate.py --repeats 20
```

The harness starts a real loopback HTTP service.
It runs 60 warmup requests per configuration.
Warmup computes classification counts once per case.
Then it measures 1,200 requests per configuration.
HTTP latency includes encoding, connection setup, service work, response transfer, and client reads.
It excludes gateway authorization, vault access, upstream work, and receipt commits.
Tier latency measures backend work only.
Percentiles use nearest-rank selection.

The first configuration uses one heuristic backend at Tier A.
The second uses the same heuristic at A and B to test cascade wiring.
B runs on 4 low-confidence cases, not the full dataset.
Its reported precision and recall are conditional on that selected subset.
This is not a Lux, Vega, or GPU benchmark.

Escalation uses the maximum of risk, personal-data probability, and task inconsistency.
The approval threshold is 0.6. The denial threshold is 0.9.
Final confidence below 0.8 also requires approval.
Earlier high-risk observations remain effective after cascading.

See [RESULTS.md](../RESULTS.md#advisor) for measurements and pending model runs.
The advisor remains disabled regardless of this test's numbers.
