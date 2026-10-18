import http from 'k6/http';
import { check, fail } from 'k6';
import { Trend } from 'k6/metrics';

const overhead = new Trend('gateway_overhead_ms', true);
const upstream = new Trend('upstream_handler_ms', true);
const baseURL = (__ENV.BASE_URL || 'http://127.0.0.1:8080').replace(/\/$/, '');
const key = __ENV.AGENTGATE_AGENT_KEY;
const vus = Number(__ENV.VUS || 16);

if (!key) {
  throw new Error('AGENTGATE_AGENT_KEY is required');
}
if (!Number.isInteger(vus) || vus < 1) {
  throw new Error('VUS must be a positive integer');
}

export const options = {
  scenarios: {
    act: {
      executor: 'constant-vus',
      vus,
      duration: __ENV.DURATION || '30s',
      gracefulStop: '10s',
    },
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(99)', 'p(99.9)', 'max'],
  thresholds: {
    http_req_failed: ['rate==0'],
    checks: ['rate==1'],
    gateway_overhead_ms: ['p(99)<1'],
  },
};

function act(worker) {
  return http.post(`${baseURL}/v1/act`, JSON.stringify({
    service: 'mock',
    action: 'echo',
    on_behalf_of: 'bench-user',
    params: { worker },
  }), {
    headers: {
      Authorization: `Bearer ${key}`,
      'Content-Type': 'application/json',
    },
    timeout: '10s',
    tags: { name: 'POST /v1/act' },
  });
}

export function setup() {
  const response = act(0);
  if (response.status !== 200) {
    fail(`warmup failed: status ${response.status}, body ${response.body}`);
  }
}

export default function () {
  const response = act(__VU);
  let body;
  try {
    body = response.json();
  } catch (_) {
    check(response, { 'valid JSON envelope': () => false });
    return;
  }
  const valid = check(response, {
    'act and upstream succeeded': (r) => r.status === 200 && body.status === 200 && body.body?.ok === true,
    'upstream timing present': () => Number.isFinite(body.body?.upstream_ns) && body.body.upstream_ns >= 0,
  });
  if (!valid) {
    return;
  }
  const upstreamMS = body.body.upstream_ns / 1e6;
  const overheadMS = response.timings.duration - upstreamMS;
  if (!check(overheadMS, { 'non-negative overhead': (value) => value >= 0 })) {
    return;
  }
  upstream.add(upstreamMS);
  overhead.add(overheadMS);
}
