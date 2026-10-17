# Registry settings

Response limits belong under each service's `upstream` settings.
Streaming permission belongs on each action.

## Rate limits

Optional `rate_limit` settings apply per API key and service.
They combine with the gateway's per-key and per-tenant buckets.
Explicitly scoped keys share human principal tenants. Wildcard keys use key-scoped tenants.
Large scope lists use a key fallback beyond the first 63 principals.
See [request admission](../README.md#request-admission) for identity bounds and startup validation.
The gateway loads them from the same registry YAML as actions.
The separate `configs/agentgate.yaml` example isn't loaded by the gateway.
Restart the gateway after changing these settings.

```yaml
services:
  example:
    base_url: https://api.example.com
    auth: {type: bearer}
    rate_limit:
      requests_per_second: 25
      burst: 50
    actions:
      list: {method: GET, path: /items}
```

Use at most 1 positive rate field: `requests_per_second`, `requests_per_minute`, or `requests_per_hour`.
Rates must be finite and non-negative. Fractional rates are accepted.
Absent or zero rates disable the service bucket. Omitted or zero `burst` selects 10 tokens.
Negative bursts and conflicting rates fail registry loading.
Rate rejection returns HTTP 429 with `rate_limited` and a whole-second `Retry-After` header.
The gateway commits its receipt before sending that response.
Scope checks run before service and tenant buckets. Concurrency denials refund all token charges.

## Response settings

```yaml
services:
  example:
    base_url: https://api.example.com
    auth: {type: bearer}
    upstream:
      max_response_bytes: 1048576
      max_stream_bytes: 67108864
      stream_idle_timeout: 60s
    actions:
      events:
        method: GET
        path: /events
        stream: true
```

| Setting | Default | Accepted values |
|---------|---------|-----------------|
| `upstream.max_response_bytes` | 1 MiB | 1024 to 67108864 bytes |
| `upstream.max_stream_bytes` | 64 MiB | 1024 to 1073741824 bytes |
| `upstream.stream_idle_timeout` | 60s | Positive Go duration |
| `actions.<name>.stream` | `false` | Boolean |

Zero limits and zero durations select defaults. Invalid settings fail registry loading.
Exactly-limit buffered bodies pass. Larger bodies return HTTP 502 with `upstream_response_too_large`.
The fixed message is `upstream response exceeds configured limit`.
The receipt records status 502 and the same error code.
The gateway reads at most the limit plus 1 byte, then closes upstream without an unbounded drain.
JSON uses `body` in the response envelope. Text uses `body_text`.
Validated JSON bytes pass through without compaction or HTML escaping.
Envelope field order stays unchanged. Text keeps standard JSON string escaping.

## Path parameters

Placeholders like `/repos/{owner}/{repo}` use values from `params`.
Each value is escaped as a single path segment with Go's `url.PathEscape`.
Slashes, query delimiters, fragment delimiters, percent signs, and spaces are encoded.
For example, `owner/repo` becomes `owner%2Frepo`, and `%2e%2e` becomes `%252e%252e`.
Inserted values aren't scanned again for placeholders.

Placeholders aren't allowed after `?`. Static query strings remain supported.
Registry loading rejects query placeholders and names the service and action without exposing template values.
The gateway also rejects these templates defensively.

Path values must be strings, integer numbers, or booleans.
Missing values, empty strings, `.` and `..` are rejected.
Objects, arrays, null, and non-integer numbers are rejected.
Integer number text stays unchanged, including large integers and integral exponent notation.
Other params can still contain objects, arrays, or null when they aren't used in the path.

Invalid path params return HTTP 400 with code `bad_request` and fixed message `invalid params`.
The receipt records status 400, policy `deny`, and error `bad_request`.
URL validation runs before vault access or token refresh.
Invalid base URLs return HTTP 500 with code `internal` and fixed message `failed to build upstream request`.
The receipt records status 500, policy `allow`, and error `internal`.
Base URL joining preserves its existing trailing-slash trimming.
The resulting URL must retain the base URL's scheme and host.
Connectors must declare path separators in templates, not inside param values.

## SSE negotiation and receipts

Set `stream: true` only for actions that may produce SSE.
The agent must send `Accept: text/event-stream`. An explicit `q=0` doesn't enable streaming.
The upstream must respond with HTTP 2xx and media type `text/event-stream`.
All other responses use buffered limits and the normal response envelope.
An action with `stream: true` remains buffered when the agent doesn't request SSE.

Before sending any HTTP headers or bytes, the gateway commits 1 receipt.
The streaming receipt records status 200 and policy `allow`.
It records upstream acceptance, not eventual stream delivery.
Receipt append failure returns HTTP 500 with `receipt_failed` and closes upstream.
The streaming response uses HTTP 200 and passes SSE bytes without an envelope.
The action description endpoint exposes the `stream` flag.

`request_timeout` bounds dispatch and retries until successful SSE headers arrive.
Afterward, only caller cancellation, caller deadlines, and stream limits stop the response.
The idle timeout starts after the receipt commits.
Blank-line SSE boundaries reset it. Line endings may use LF, CRLF, or CR.
Partial events don't extend the timeout.
The separate byte limit stops oversized streams. It doesn't change an already-sent HTTP status.
Client disconnects stop forwarding and close upstream.
Stream completion leaves a 5s write deadline for the final HTTP framing.

The final server log includes `sha256`, `bytes`, and `reason`.
Reasons include `completed`, `idle_timeout`, `max_stream_bytes`, and transport or client failures.
The hash covers bytes accepted by downstream writes, not proof of client delivery.
Receipt encoding stays unchanged. Logs aren't signed stream attestations.
See the [v2 proposal](spec/proposals/receipt-stream-digest.md) for a possible signed completion artifact.
