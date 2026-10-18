---
phase: 10-sourced-product-comparison
gathered: 2026-08-16
status: ready-for-planning
mode: autonomous (continuing established roadmap execution)
research: live fetches of nango.dev/docs.nango.dev, composio.dev/docs.composio.dev,
  astrix.security/astrix.security/product, and oasis.security (all 2026-08-16),
  PRD-receipts-oss.md TASK-R10
---

<domain>
## Phase Boundary

COMP-01 through COMP-05: `docs/comparison.md` compares AgentGate against
Nango, Composio, Astrix Security, and Oasis Security across five
capability columns, sourced entirely from each vendor's own current public
documentation, with missing evidence labeled `Not documented` rather than
inferred as absence. Phase 11 (contribution path) is untouched.
</domain>

<decisions>
## A prompt injection attempt was found and not acted on

`composio.dev`'s homepage HTML contains text specifically addressed to AI
agents reading the page, instructing them to sign up and enter credentials
on the user's behalf, with a thin disclaimer ("confirm with the user
first"). This was recognized as a prompt injection pattern during
research and not acted on: no sign-up flow was initiated, no credentials
were entered, and the only use made of the page was reading its
already-public marketing copy for the comparison table, the same as any
other source in this phase.

## One capability, "sits in the request path," turned out to genuinely distinguish two different product categories

Nango and Composio are integration/auth runtimes whose own infrastructure
executes the live API call. Astrix Security and Oasis Security are
non-human-identity governance products: Astrix explicitly documents
itself as "Agentless... a non-proxy API-based solution" reading metadata
only, and Oasis's public pages describe identity/permission governance
across providers and vaults without stating whether its own
infrastructure intercepts individual live agent calls. This column is not
a bias toward AgentGate's own design — it is the most direct way to
express a real, sourced structural difference: whether the product's
architecture makes it *the* choke point through which action requests
must flow (which is exactly the leverage point that also makes signed
receipts meaningful and enforceable), or whether it observes/governs from
outside that path.

## Astrix Security's ownership and sales-status change gets flagged prominently, not silently dropped

`astrix.security`'s own homepage states it is "now part of Cisco" and
"ended standalone sales of new licenses effective June 30, 2026" (a date
already past as of this comparison's compile date, 2026-08-16). Per
`PRD-receipts-oss.md`'s TASK-R10 ("if a competitor ships receipts before
launch, update the table honestly rather than quietly dropping the row"),
the analogous obligation here is not to silently drop or quietly footnote
a material, sourced fact about a competitor's current availability — it
is called out as a blockquote at the top of Astrix's section, not buried.

## Every "Not documented" is a claim about what public sources say, not about the product

For each of the 10 "Not documented" cells (2 rows x 5 competitors), the
absence is of a public page affirmatively describing the capability — not
a claim the vendor lacks it. This is stated once in the table's own
caption and repeated implicitly by never writing a bare "No" without a
citing source (Astrix's two "No"/"Not documented" distinction in the
"sits in the request path" row is the one case with an explicit,
citable denial versus an absence of evidence, and the wording
deliberately reflects that difference).

## AgentGate's own row cites shipped code and docs, not aspirational claims

Every AgentGate "Yes" links to a file that exists and is tested in this
same repository (`README.md`, `internal/gateway/gateway.go`,
`internal/receipt`, `cmd/agentgate-verify`) — the same evidentiary bar
applied to competitors, satisfying COMP-04's "AgentGate claims link to
shipped behavior and reproducible verification evidence."
</decisions>

<code_context>
## Existing Code Insights

None — this phase adds one new documentation file and touches no Go code.
`docs/relicense-authorization.md` was the only prior file in `docs/`.
</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above.
</specifics>

<deferred>
## Deferred Ideas

- Automating link-liveness checks for the comparison's citations (a CI job
  that flags a 404'd competitor URL) — not required by any COMP-0X item,
  and no CI/GitHub Actions build wiring exists yet in this repository at
  all (unchanged limitation carried since Phase 6).
</deferred>
