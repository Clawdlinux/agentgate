# Advisor evaluation

All 60 cases are synthetic. No real receipt logs exist yet.
Each state uses the gateway's exact `service`, `action`, and `params` shape.
Cases include 33 escalation labels and 27 non-escalation labels.
They are not held-out model evaluation data.
Some actions exist only in the synthetic fixtures.

Run from the repository root:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 bench/advisor/evaluate.py --repeats 20
```

The harness starts a real loopback HTTP service.
It runs 60 warmup requests per configuration.
Warmup computes classification counts once per case.
Then it measures 1,200 requests per configuration.
HTTP latency includes connection setup, HTTP framing, service work, response transfer, and client reads.
Payload serialization and response JSON decoding are outside the interval.
It excludes gateway authorization, upstream calls, and receipt commits.
Tier latency measures backend work only.
Percentiles use nearest-rank selection.

The first configuration uses one heuristic backend at A.
The second repeats that heuristic at A and B to test cascade wiring.
There is no task signal in the gateway state.
Payload `stated_task` fields cannot increase heuristic confidence.
The heuristic's low confidence makes every request require approval under default thresholds.
Consequently, all 60 cases reach B in the test cascade.
This is not a Lux, Vega, or GPU benchmark.

Escalation uses risk, personal-data probability, and task inconsistency.
The approval threshold is 0.6. The denial threshold is 0.9.
Final confidence below 0.8 also requires approval.
The conservative heuristic cannot distinguish the 27 acceptable requests without task context.

See [RESULTS.md](../RESULTS.md#advisor) for measurements and pending model runs.
The advisor remains disabled regardless of these test numbers.
