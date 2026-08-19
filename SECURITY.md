# Security Policy

## Reporting a Vulnerability

Email **security@clawdlinux.org**. Please don't open a public GitHub issue for a
security vulnerability — that gives every reader a working exploit before a fix
ships.

Alternatively, use [GitHub Private Vulnerability
Reporting](https://github.com/Clawdlinux/agentgate/security/advisories/new) on
this repository, which is enabled and reaches the same people.

## Response Commitment

- **Acknowledgement:** within 3 working days of the report.
- **Initial assessment:** within 10 working days — whether it's confirmed, its
  severity, and a rough timeline.

AgentGate is maintained by one person. These windows are what can actually be
delivered, not an aspirational SLA.

## Supported Versions

AgentGate is pre-1.0. Only the latest minor release gets security fixes.

| Version | Supported |
| --- | --- |
| latest 0.x minor | Yes |
| anything older | No |

## Scope

**In scope:**

- The gateway request path (`/v1/act` and the OAuth token flow)
- The token vault (encryption at rest, key handling)
- The receipt ledger and its signing (`internal/receipt`, `internal/signer`)
- The offline verifier (`agentgate-verify`)
- The Biscuit delegation verifier (`internal/delegation`)
- The admin dashboard (`cmd/agentgw/web/dashboard`)
- The export endpoints (`/v1/receipts/export`, `/v1/receipts/pubkey`)

**Out of scope:**

- Anything that requires an already-compromised `X-Admin-Secret` — an attacker
  who already has that secret has already won; report the exposure that got
  them the secret, not what they can do once they have it.
- Findings against the example `configs/` shipped for local development, which
  are not meant to be run as-is in production.
- Self-XSS (a user pasting something into their own browser console).
- Denial of service via unbounded local resource use (e.g. sending a very
  large request body) — real, but tracked as a hardening issue, not a
  security vulnerability report.

## Disclosure

Coordinated disclosure, 90 days by default from acknowledgement, negotiable if
a fix needs more time or the reporter needs less.

## Known Design Decisions (Not Bugs)

- **The dashboard HTML at `/dashboard/` serves without authentication.** This
  is intentional, not an oversight: the HTML/CSS/JS itself carries no secrets
  and no data. Every actual data call the dashboard makes requires the caller
  to supply a valid `X-Admin-Secret` via the `/v1/receipts/export` endpoint,
  which is where the real access control lives. Please don't re-report "the
  dashboard page loads without a login."
