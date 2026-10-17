# Registry response settings

Response limits belong under each service's `upstream` settings.
Streaming permission belongs on each action.

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

The final server log includes `sha256`, `bytes`, and `reason`.
Reasons include `completed`, `idle_timeout`, `max_stream_bytes`, and transport or client failures.
The hash covers bytes accepted by downstream writes, not proof of client delivery.
Receipt encoding stays unchanged. Logs aren't signed stream attestations.
See the [v2 proposal](spec/proposals/receipt-stream-digest.md) for a possible signed completion artifact.
