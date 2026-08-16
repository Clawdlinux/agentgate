# Domain Pitfalls

**Domain:** AgentGate cryptographic receipts and OSS launch
**Researched:** 2026-08-11
**Overall confidence:** HIGH
**Primary concern:** Product claims must not exceed the evidence a receipt can prove.

This is engineering research, not legal advice.

## Launch Claim Boundary

| Topic | Supportable claim | Unsupported claim |
|---|---|---|
| Signature | A pinned AgentGate key signed these canonical bytes. | The upstream service independently confirmed the action. |
| Chain | Supplied records form a continuous signed chain. | No records exist before, after, or outside the supplied chain. |
| Parameters | The receipt commits to a parameter digest. | The receipt is replayable without the original parameters. |
| Principal | A verified grant identified this principal. | A caller-provided principal proves human authorization. |
| Time | AgentGate recorded this timestamp. | A trusted timestamp authority proved the action time. |
| Privacy | Raw parameters are omitted from the receipt. | Hashing makes the receipt anonymous or safe to publish. |
| Offline verification | A pinned keyring verifies signatures without network access. | A bundled public key establishes signer identity. |

Launch copy should say `gateway-attested`, `tamper-evident`, and `verifies the supplied artifact`.

Launch copy should not say `tamper-proof`, `complete`, `non-repudiable`, `anonymous`, or `replayable`.

## Critical Pitfalls

### Pitfall 1: Caller-controlled identity makes the authorization claim false

**What goes wrong:** The receipt signs a claimed human principal, not a verified authorization fact.

**Why it happens:** `on_behalf_of` comes directly from the request body.

The production gateway authenticates against `Config.AgentKeys`, a plaintext map.

It does not use the scoped SQLite `KeyStore` or its service and principal checks.

**Consequences:** An agent can name another principal and receive a valid signature over that false attribution.

The receipt then proves AgentGate signed a caller assertion. It does not prove human consent.

**Warning signs:**

- Receipt construction reads `req.OnBehalfOf` without a verified grant object.
- Tests omit cross-principal and cross-service attempts.
- The receipt has no grant identifier, grant version, or authorization source.
- Product copy says the human authorized the action.

**Prevention:**

- Wire the SQLite authentication middleware into the owning request path.
- Enforce `AllowedUsers` and `AllowedServices` before vault lookup.
- Derive `agent_key_id` from the verified key record.
- Derive the principal from a verified grant binding.
- Add a stable grant identifier or policy version to the signed receipt.
- Label caller input as `claimed_principal` until verification succeeds.
- Do not equate account linking with consent for every later action.

**Test evidence required:**

- A key scoped to principal A cannot invoke for principal B.
- A key scoped to GitHub cannot invoke Slack.
- A spoofed body never changes the signed verified principal.
- Denied attempts record the verified key ID and denial reason.
- An allowed receipt references the exact grant used.

**Phase:**

R4, request-path wiring. Treat scoped authorization as an R4 prerequisite.

**Confidence:** HIGH. Current gateway and auth packages show this mismatch directly.

### Pitfall 2: SQLite and a SaaS side effect cannot commit atomically

**What goes wrong:** The upstream action succeeds, then receipt persistence fails or the process crashes.

Writing after the call loses evidence. Writing before the call cannot record the final outcome.

A database transaction cannot roll back a completed GitHub, Slack, or Stripe action.

**Consequences:** The claim that every action has one final receipt becomes false during a failure window.

Returning an error may also cause an agent to repeat an already completed action.

**Warning signs:**

- One receipt is inserted only after `HTTPClient.Do` returns.
- The response is written before the receipt transaction commits.
- Receipt failures are logged and ignored.
- A SQLite transaction remains open during the network call.
- Retry behavior lacks an interaction ID or upstream idempotency support.

**Prevention:**

- Persist a durable `attempt` record before contacting upstream.
- Fail closed when the attempt record cannot commit.
- Finalize the attempt with `succeeded`, `failed`, or `outcome_unknown`.
- Keep a stable interaction ID across retries and recovery.
- Send the HTTP response only after terminal persistence succeeds.
- Never hold the SQLite write transaction across the network call.
- Document that crash recovery may preserve an unknown outcome.
- Use upstream idempotency only where that connector supports it.

This changes the model from one perfect event to a durable attempt state machine.

**Test evidence required:**

- Kill before attempt commit. The upstream sees no call.
- Kill after attempt commit and before the call. Recovery finds the attempt.
- Kill after upstream response and before finalization. Recovery reports `outcome_unknown`.
- Kill after finalization and before response delivery. Supported retries do not duplicate actions.
- Simulate disk-full and locked-database failures at every boundary.

**Phase:**

R4, request-path wiring. This phase needs focused failure-state research.

**Confidence:** HIGH. SQLite and SaaS calls occupy separate transaction domains.

### Pitfall 3: Concurrent writers and restart logic fork the sequence

**What goes wrong:** Two requests read the same tail and compute competing next receipts.

A restart can reset `seq` or `prev_hash` when tail loading fails.

SQLite WAL permits concurrent readers but only one writer.

That rule does not make application-level read, hash, and insert steps atomic.

**Consequences:** Duplicate sequences, broken links, `SQLITE_BUSY`, or two valid continuations can result.

A restored snapshot can create another valid branch under the same signing key.

**Warning signs:**

- Code runs `SELECT MAX(seq)` outside the insert transaction.
- `sql.DB` may use several connections for receipt writes.
- Startup treats any tail-read error as an empty ledger.
- Multiple AgentGate processes can open the same SQLite file.
- A restored backup resumes under the same ledger ID and key.
- The only concurrency test sends 100 requests once.

**Prevention:**

- Serialize chain mutation through one synchronous writer.
- Acquire the write lock before reading the current tail.
- Allocate sequence, compute hashes, and insert within one transaction.
- Add unique constraints for sequence and entry hash.
- Fail startup when a nonempty tail cannot be read or verified.
- Store a random immutable ledger ID inside every entry hash.
- Prevent multiple writer processes for one ledger file.
- Treat backup restoration as a new ledger generation by default.
- Keep busy retries bounded and visible.

**Test evidence required:**

- Repeated concurrent tests produce exactly sequences `1..N`.
- Race-enabled tests show no shared-tail race.
- A second writer process is rejected or safely serialized.
- Restart resumes from the exact committed tail.
- A corrupt tail causes startup failure.
- Clock rollback does not change sequence ordering.
- Snapshot restoration cannot silently continue as the original ledger.

**Phase:**

R4, synchronous receipt storage and restart recovery.

**Confidence:** HIGH. SQLite documents one-writer behavior and transaction upgrade failures.

### Pitfall 4: Self-authenticating public keys make verification meaningless

**What goes wrong:** A forged log includes a public key that verifies its own forged signatures.

A current-key endpoint also breaks historical verification after rotation.

**Consequences:** Cryptographic verification passes without identifying the signing AgentGate instance.

Old receipts become unverifiable, or a substituted key history becomes trusted.

**Warning signs:**

- The verifier trusts a public key embedded only inside the export.
- `signer_kid` selects any key from the same untrusted artifact.
- `/v1/receipts/pubkey` returns only the active key.
- Rotation replaces a key instead of preserving history.
- Restart generates a signer when private-key loading fails.
- The offline verifier fetches keys from the network.

**Prevention:**

- Require a separately pinned keyring or fingerprint.
- Treat embedded public keys as transport data, not trust anchors.
- Make `kid` content-derived or collision-checked.
- Preserve every historical public key needed by retained receipts.
- Bind key activation to sequence boundaries.
- Record rotation with old-key and new-key authorization.
- Keep sequence and `prev_hash` continuous through rotation.
- Persist the encrypted private key before the first receipt.
- Fail startup when the wrapping key or signer is unavailable.
- Bind encrypted signer material to its purpose and `kid` using authenticated data.

The current command uses an in-memory vault. Its fallback key changes on restart.

Compose sets `AGENTGATE_VAULT_KEY`, while the command reads `VAULT_ENCRYPTION_KEY`.

**Test evidence required:**

- Verification succeeds offline with receipts and a previously pinned keyring.
- Networking is blocked during that test.
- A substituted public key fails.
- An unknown, duplicate, or aliased `kid` fails.
- A chain spanning two rotations verifies.
- Removing a historical key fails clearly.
- Restart preserves the signer and chain tail.
- A wrong wrapping key fails before serving requests.

**Phase:**

R3 owns key lifecycle. R5 must enforce the trust model.

**Confidence:** HIGH. Signature verification always depends on an authenticated trust anchor.

### Pitfall 5: A valid local chain does not prove completeness

**What goes wrong:** Deleting final records leaves a shorter chain that still verifies.

Restoring an older snapshot can produce two valid forks from one signed head.

An operator holding the signer can rewrite and resign the entire ledger.

**Consequences:** Claims about all deletions, append-only storage, or complete history become misleading.

`No sequence gaps` only describes supplied rows. It does not prove every request was recorded.

**Warning signs:**

- Full verification accepts a chain ending anywhere.
- A bounded export reports the same success as a complete ledger.
- The verifier lacks an expected ledger ID, genesis, head, or last sequence.
- Marketing says all deletions are detected.
- SQLite permissions are described as append-only protection.

**Prevention:**

- Define full-log and bounded-segment verification as different claims.
- Require an expected ledger ID and trusted head for completeness checks.
- Bind first sequence, last sequence, count, and head in an export manifest.
- State that local verification cannot detect unseen suffixes or forks.
- Compare heads from independent times or parties when fork detection matters.
- Keep external checkpoint publication as the stronger planned control.
- Do not call mutable SQLite storage append-only.

RFC 9162 uses signed heads and consistency proofs for append-only evidence.

It notes that inconsistent views require comparison between observers.

**Test evidence required:**

- Deleting a middle row fails.
- Deleting the first row fails full-log verification.
- Deleting the final row fails when an expected head is supplied.
- The truncated file passes only an explicitly labeled segment check.
- Two forks are detected when both heads are compared.
- Re-signing under an unpinned key never passes.

**Phase:**

R5 owns verifier semantics. R6 owns exported boundaries. R10 owns claim wording.

**Confidence:** HIGH. Chain continuity cannot prove an unseen suffix exists.

### Pitfall 6: Ambiguous canonicalization signs a different action

**What goes wrong:** The signer, gateway, exporter, and verifier derive different bytes from equivalent inputs.

The request decodes parameters into `map[string]interface{}`.

Default decoding converts numbers to `float64` and keeps legacy JSON behavior.

Duplicate names, trailing values, large integers, invalid Unicode, and nil values need explicit rules.

The reference hash format also lacks an AgentGate protocol tag and version.

**Consequences:** Honest receipts fail across implementations, or malicious inputs exploit parser disagreement.

A signature may also cross another protocol using the same key and message shape.

**Warning signs:**

- `json.Marshal(map)` is described as the canonical protocol.
- Request decoding calls `Decode` once without checking end-of-input.
- Duplicate JSON names are accepted.
- Numbers near `2^53` round before hashing.
- `signer_kid`, ledger ID, or version is outside `entry_hash`.
- Empty and absent delegation chains encode identically by accident.
- One key signs unrelated artifacts without context.

**Prevention:**

- Publish one versioned byte-level format before implementation.
- Include an `agentgate.receipt.entry.v1` domain prefix.
- Include ledger ID, schema version, and signer `kid` in the hash.
- Sign a separately tagged `agentgate.receipt.signature.v1` message.
- Domain-separate the parameter commitment.
- Choose JCS for parameters or define another exact form.
- Reject duplicate names, trailing JSON, invalid UTF-8, and unsupported numbers.
- Preserve numeric literals with `UseNumber` before canonicalization.
- Define byte order, signed integers, limits, arrays, and optional fields.
- Keep execution and hashing normalization identical.

RFC 8032 recommends fixed contexts for separating signature uses.

RFC 8785 defines duplicate-name, number, sorting, Unicode, and UTF-8 rules.

**Test evidence required:**

- Two independent encoders match one checked-in golden vector.
- A non-Go verifier validates that vector.
- Fuzz tests compare encode, decode, and re-encode behavior.
- Duplicate keys and trailing JSON fail.
- Tests cover `2^53`, negative zero, exponents, Unicode, nil, and empty arrays.
- Tampering with `kid`, version, ledger ID, or presence fails.
- A signature from another artifact domain fails receipt verification.

**Phase:**

R2, receipt type and canonical encoding.

**Confidence:** HIGH. Current Go and RFC documentation identify these differences.

### Pitfall 7: Denial paths bypass the receipt boundary

**What goes wrong:** Unauthorized, malformed, missing-token, expired-token, rate-limited, and upstream-error paths return before persistence.

The current rate limiter is separate middleware. The audit logger is absent from `gateway.Config`.

The integration setup creates an audit logger but never calls it.

**Consequences:** The schema advertises `deny` and `rate_limited`, while those decisions remain unrecorded.

Recording every unauthenticated byte instead creates a disk-exhaustion path.

**Warning signs:**

- Receipt creation appears only on the success path.
- Middleware can return without a receipt hook.
- Denial receipts require a principal that is not known.
- Malformed bodies are unbounded before hashing.
- Invalid-request flooding grows the database without limits.

**Prevention:**

- Define the event universe before coding.
- Separate network noise, authenticated attempts, policy decisions, and upstream outcomes.
- Persist every authenticated action attempt and every policy denial.
- Decide whether authentication failures belong in receipts or security logs.
- Make unknown identity fields explicit.
- Centralize outcome capture around every owning return path.
- Put body limits before parsing and receipt creation.
- Rate-limit receipt-producing denials without dropping admitted decisions.
- Store stable reason codes instead of raw error text.

**Test evidence required:**

- A table covers every return path and middleware branch.
- Each in-scope request produces exactly one durable attempt.
- Rate-limited requests produce the documented receipt outcome.
- Unknown credentials never appear as a verified agent.
- Malformed and oversized bodies follow the documented event policy.
- A denial flood stays within configured storage limits.

**Phase:**

R4, request-path integration.

**Confidence:** HIGH. Current early returns and middleware boundaries are visible in source.

### Pitfall 8: The verifier reproduces the signer bug

**What goes wrong:** Signer and verifier share one encoder, parser, or permissive default.

Both sides then agree on the same incorrect interpretation.

Loose JSONL parsing may accept duplicates, unknown versions, wrong order, or numeric overflow.

**Consequences:** Tests pass while an independent implementation rejects the artifact.

An offline verifier may also mutate SQLite or fetch keys unexpectedly.

**Warning signs:**

- Signer tests call only the production verifier.
- There is no external golden vector.
- Unknown fields or versions are silently ignored.
- The verifier sorts records instead of rejecting wrong order.
- SQLite verification runs migrations or write pragmas.
- Missing keys trigger network discovery.

**Prevention:**

- Keep the wire specification independent from Go structs.
- Add a second implementation or independently generated fixtures.
- Reject duplicate fields, unknown versions, algorithms, and keys.
- Enforce order, continuity, sizes, and exact binary encodings.
- Open SQLite read-only inside one read transaction.
- Never run migrations during verification.
- Require explicit keyring input for offline mode.
- Bound line length, record count, nesting, and total bytes.
- Keep exit codes stable and machine-readable.

**Test evidence required:**

- Official Ed25519 vectors pass.
- Cross-language receipt vectors pass.
- Differential tests compare both implementations.
- A malformed JSONL corpus fails deterministically.
- Verification works with networking disabled.
- The source database hash stays unchanged.
- Exit `1` means verification failure. Exit `2` means input failure.

**Phase:**

R5, offline verifier.

**Confidence:** HIGH. Independent verification requires separate evidence and strict parsing.

### Pitfall 9: Bounded exports omit material records

**What goes wrong:** A valid range proves only internal continuity for that range.

The first row's `prev_hash` does not prove the intended predecessor was supplied.

A live SQLite file copied without its WAL may omit committed receipts.

**Consequences:** An auditor receives a valid but incomplete artifact and interprets exit `0` as completeness.

Concurrent writes can also make separately queried metadata and rows disagree.

**Warning signs:**

- Export returns bare JSONL without a manifest.
- Requested and actual bounds are not signed.
- The endpoint lacks authorization, count limits, or audit events.
- Export reads metadata and rows outside one snapshot.
- Documentation recommends copying only the `.db` file during WAL activity.

**Prevention:**

- Export from one SQLite read transaction.
- Return a signed manifest beside JSONL records.
- Bind requested bounds, actual bounds, count, ledger ID, and head.
- Include the historical keyring fingerprint.
- Label partial ranges as segments.
- Require trusted boundary inputs for segment completeness.
- Protect export with admin authorization and range caps.
- Set no-store headers and audit export operations.
- Use SQLite backup or a completed checkpoint for snapshots.
- Keep required `.db`, `-wal`, and `-shm` state together.

**Test evidence required:**

- A full export verifies against its manifest head.
- Removing the first or last record fails manifest verification.
- Changing bounds, count, or ledger ID fails.
- A live export stays consistent during writes.
- Copying only an active WAL database is rejected or documented unsafe.
- Unauthorized and oversized export requests fail.

**Phase:**

R6, export endpoint. R5 must distinguish full logs from segments.

**Confidence:** HIGH. SQLite documents the WAL file as persistent database state.

### Pitfall 10: Receipt exports leak identity and low-entropy parameters

**What goes wrong:** SHA-256 hides raw parameters but remains vulnerable to educated guesses.

Human principal, agent key ID, service, action, time, error, and delegation structure remain visible.

Stable hashes also permit correlation across exports.

**Consequences:** The export can expose personal, business, and security-sensitive behavior.

Calling it anonymous creates legal and product risk.

**Warning signs:**

- Privacy review considers only raw-body omission.
- Tests use emails, channels, repositories, or small enumerated values.
- Free-form upstream errors enter receipts.
- Exports have broad access or unlimited retention.
- Documentation says hashes remove data-protection duties.

**Prevention:**

- Describe `params_sha256` as a commitment, not anonymization.
- Perform a field-level data-minimization review.
- Replace free-form errors with stable reason codes.
- Keep secrets, URLs, headers, and response bodies out.
- Authenticate exports and define retention behavior.
- Assess dictionary and linkage risk for each connector.
- Consider salted, keyed, or tokenized pseudonyms where matching is unnecessary.
- Document any tradeoff that prevents digest recomputation.
- Treat pseudonymized receipts as protected data.

European Commission and ICO guidance says re-identifiable pseudonyms remain personal data.

ICO guidance warns about unsalted hashes and dictionary attacks.

**Test evidence required:**

- A dictionary test demonstrates expected low-entropy risk.
- Secret canaries never appear in SQLite, JSONL, logs, or errors.
- Export authorization and no-store behavior are tested.
- Retention tests delete exports without mutating receipts.
- A privacy fixture inventories every exported field and purpose.

**Phase:**

R2 defines fields. R6 owns export controls. Launch review owns wording.

**Confidence:** HIGH. Official privacy guidance covers hashing and pseudonymization directly.

### Pitfall 11: Free-form delegation summaries permit chain splicing

**What goes wrong:** A `delegation_chain []string` can describe lineage without proving it.

Copying identities or caveats from a valid token does not bind them to this invocation.

Using Biscuit also does not make AgentGate compliant with a separate JWT draft.

**Consequences:** A receipt can display a plausible chain that was never authorized.

An attacker may splice a valid attenuation from another parent or root.

**Warning signs:**

- The receipt stores display strings instead of verified token bytes.
- Authorization reads claims before verifying the Biscuit root and blocks.
- Parent identity is compared by text only.
- The request body supplies delegation lineage.
- Documentation calls Biscuit an AAT draft implementation.
- The dependency uses the old `github.com/biscuit-auth/biscuit-go` path.

**Prevention:**

- Verify the complete Biscuit token against a configured root key.
- Run authorization against exact service, action, principal, and parameters.
- Derive receipt lineage only from the verified token.
- Sign a digest of the complete serialized token or block chain.
- Include root key ID, grant ID, leaf digest, and block count.
- Reject a block under a different parent or root.
- Pin `github.com/eclipse-biscuit/biscuit-go/v2` to a supported release.
- Describe IETF drafts as work in progress with exact revisions.
- Do not claim compliance without an interoperability profile.

The AAT draft binds each child to its parent signing input with `par_hash`.

The draft explicitly describes Biscuit as a different token format.

OBO revision `-02` expired on 2026-02-27.

AAT revision `-00` expires on 2026-09-17.

**Test evidence required:**

- A valid parent and child authorize the intended invocation.
- A child from chain A cannot enter chain B.
- The same display facts under another root fail.
- Tampering with any serialized block fails.
- A valid token with unauthorized arguments is denied.
- Unknown roots, expired grants, and unsupported versions fail closed.
- Receipt lineage matches the exact accepted token.

**Phase:**

R7, attenuated delegation.

**Confidence:** HIGH for binding requirements. MEDIUM for changing draft details.

### Pitfall 12: Apache relicensing lacks proven provenance

**What goes wrong:** Replacing four BSL headers does not establish rights over every repository file.

Git history currently shows one distinct author. That does not prove copyright ownership or employer consent.

Ported code, generated content, examples, and dependencies may carry separate obligations.

**Consequences:** The repository can publish an Apache label without authority to grant that license.

Contributor outreach then starts from an uncertain baseline.

**Warning signs:**

- Relicensing evidence is only `git shortlog` output.
- No owner representation covers employment and imported code.
- The new `LICENSE` ignores third-party notices.
- Ported `chain.go` code loses attribution.
- IETF sample code is copied without checking IETF Trust terms.
- DCO sign-off is treated as retroactive permission.

**Prevention:**

- Obtain a written representation from every copyright owner.
- Confirm employer, contractor, and assignment status.
- Inventory source, docs, generated files, assets, and vendored material.
- Preserve applicable copyright and attribution notices.
- Add the unmodified Apache 2.0 license.
- Keep NOTICE limited to required attribution.
- Record relicensing evidence privately.
- Audit direct and transitive dependency licenses.
- Treat future DCO sign-off as provenance, not assignment.
- Get counsel for unresolved ownership questions.

**Test evidence required:**

- No BSL text remains in distributed files.
- Every distributed file has a provenance classification.
- LICENSE and NOTICE match release contents.
- Ported code retains required attribution.
- Dependency license inventory passes in CI.
- A source archive contains all required license files.

**Phase:**

R1, Apache 2.0 relicense. This phase blocks public contribution work.

**Confidence:** HIGH for the engineering gap. Legal authority needs owner confirmation.

### Pitfall 13: Competitor and product claims outrun evidence

**What goes wrong:** The PRD says nobody ships the claimed capability.

Missing public documentation does not prove a competitor lacks private or new functionality.

`Replayable` is also inaccurate when receipts contain only a parameter digest.

**Consequences:** Launch copy becomes stale, misleading, or legally risky.

It can also damage trust with maintainers and buyers.

**Warning signs:**

- Copy uses `nobody`, `only`, `first`, or `cannot` without dated evidence.
- A comparison marks `no` because research found no documentation.
- Secondary blogs support competitor rows.
- Links point to homepages instead of exact feature docs.
- A product claim has no acceptance test.
- Receipts are described as upstream proof.

**Prevention:**

- Maintain a dated claim ledger with owner, wording, source, and review date.
- Use first-party competitor sources for every positive claim.
- Mark missing negative evidence as `unknown`.
- Identify the exact comparison basis and product edition.
- Recheck every row immediately before launch.
- Remove absolute market claims unless independently substantiated.
- Replace `replayable` with `verifiable commitment`.
- State trust assumptions and completeness limits beside comparisons.
- Separate feature presence from performance claims.

FTC guidance requires truthful, non-deceptive, evidence-based claims.

Its comparative policy requires clear comparison bases and substantiation.

**Test evidence required:**

- Every comparison cell links to a dated first-party source.
- A reviewer can reproduce every classification.
- Link checks pass on launch day.
- Security and legal reviewers approve the claim ledger.
- README, website, launch post, and comparison use matching wording.

**Phase:**

R10, comparison table. Apply the same gate to R9 and launch copy.

**Confidence:** HIGH for claim discipline. Competitor capabilities remain time-sensitive.

### Pitfall 14: The five-minute quickstart demonstrates another system

**What goes wrong:** The current Compose path does not persist the vault or receipt database.

It mounts `/data`, but the command creates `NewMemoryStore` and never opens SQLite.

Compose sets an environment variable the command does not read.

The container requests `/etc/agentgate/configs/services.yaml`, which is absent.

**Consequences:** A demo may work once, lose keys after restart, or fail before producing a receipt.

A timed path can hide host dependencies, cached images, or OAuth credentials.

**Warning signs:**

- The quickstart never restarts the container.
- It depends on host Go for verification.
- The timer excludes OAuth application setup or image downloads.
- Fixed development secrets appear as production defaults.
- A warm machine is called a clean machine.

**Prevention:**

- Make the command use persistent SQLite and the mounted path.
- Use one documented vault-key environment variable.
- Validate key length and refuse missing production keys.
- Ship the verifier in the image or release artifact.
- Ensure the referenced service registry exists.
- Define exactly when timing starts and stops.
- Test with empty Docker caches and no host toolchain.
- Separate development defaults from production guidance.
- Include restart continuity in quickstart acceptance.

**Test evidence required:**

- `docker compose config` succeeds from a fresh clone.
- A fresh build starts without missing files.
- One action produces a verifiable receipt using shipped tools.
- Restart preserves tokens, signer, ledger ID, and sequence.
- Another operator completes the cold timed path.
- Default secrets trigger a development-only warning.

**Phase:**

R9, five-minute quickstart.

**Confidence:** HIGH. Current command, Compose, and Dockerfile disagree directly.

## Moderate Pitfalls

### Pitfall 15: Receipt fields conflate policy and outcome

**What goes wrong:** One status and error string represent several decision layers.

An upstream `403` is not an AgentGate policy denial.

**Warning signs:**

Policy, gateway, upstream, and receipt persistence share one status field.

**Prevention:**

Define separate policy, gateway, upstream, and receipt-state fields with fixed enums.

**Test evidence required:**

Exercise policy deny, token failure, network failure, upstream `403`, and persistence failure separately.

**Phase:**

R2 defines semantics. R4 populates them.

**Confidence:** HIGH.

### Pitfall 16: Signed timestamps are mistaken for trusted time

**What goes wrong:** The signer controls its clock and reports its upstream observation.

**Warning signs:**

Copy says the receipt proves action time or proves SaaS state changed.

**Prevention:**

Call timestamps gateway-recorded. Call status a gateway observation. Avoid trusted-time language.

**Test evidence required:**

Clock rollback preserves sequence. Mocked upstream responses are not described as independent evidence.

**Phase:**

R2, R5, and R10.

**Confidence:** HIGH.

### Pitfall 17: Migration creates a false historical boundary

**What goes wrong:** Existing `audit_log` rows may appear as cryptographic history after migration.

The migration runner also lacks a schema-version table.

**Warning signs:**

The first receipt predates the feature, or migrations rerun without recorded state.

**Prevention:**

Start a documented genesis at migration time. Never synthesize receipts from legacy rows.

Use transactional, version-tracked, idempotent migrations. Keep both APIs separate.

**Test evidence required:**

Upgrade a populated `001` database twice. Verify one genesis and unchanged legacy rows.

**Phase:**

R4, migration `002_receipts.sql`.

**Confidence:** HIGH.

### Pitfall 18: Correct synchronous writes become unavailable

**What goes wrong:** Busy locks, checkpoints, slow disks, or full disks block every action.

**Warning signs:**

No timeout, disk quota, busy metrics, checkpoint tests, or p99 measurement exists.

**Prevention:**

Set bounded deadlines. Fail before upstream execution. Expose storage health. Monitor WAL and disk size.

Keep one writer. Benchmark contention and checkpoints.

**Test evidence required:**

Load tests cover lock contention, checkpoint spikes, disk-full, slow storage, and cancellation.

**Phase:**

R4.

**Confidence:** HIGH.

### Pitfall 19: Dependency drift weakens the receipt path

**What goes wrong:** A moved module, unsupported release, or SQLite downgrade changes security behavior.

The current Biscuit module path is `github.com/eclipse-biscuit/biscuit-go/v2`.

Its security policy currently supports version `2.2.0`.

The pinned `go-sqlite3` source embeds SQLite `3.53.0`.

That SQLite version includes the 2026 WAL-reset fix.

**Warning signs:**

The PRD import path is copied directly, or CI never records `sqlite_version()`.

**Prevention:**

Pin reviewed versions. Track upstream security policies. Assert linked SQLite versions in releases.

**Test evidence required:**

Build metadata records versions. CI rejects unsupported Biscuit or vulnerable SQLite versions.

**Phase:**

R4 for SQLite. R7 for Biscuit.

**Confidence:** HIGH as of 2026-08-11.

### Pitfall 20: DCO sign-off is called a cryptographic signature

**What goes wrong:** `git commit -s` adds a DCO line. It does not create a GPG signature.

The DCO also records contributor identity information indefinitely.

**Warning signs:**

CONTRIBUTING says `signed commit` without naming DCO sign-off or privacy effects.

**Prevention:**

Use `DCO sign-off` wording. Explain certification and the public identity record. Enforce it in CI.

**Test evidence required:**

A missing sign-off fails. A valid sign-off passes. Docs distinguish `-s` from `-S`.

**Phase:**

R11, contributor guidance.

**Confidence:** HIGH. The DCO text defines both facts.

### Pitfall 21: Error text leaks secrets and injects logs

**What goes wrong:** Errors may contain URLs, identifiers, query values, or attacker-controlled newlines.

**Warning signs:**

Receipt `error` stores `err.Error()` or response bodies directly.

**Prevention:**

Store enumerated error codes. Sanitize display messages outside signed semantics.

**Test evidence required:**

Secret canaries and CRLF payloads never appear unescaped in receipts, JSONL, or terminal output.

**Phase:**

R2 and R4.

**Confidence:** HIGH.

## Minor Pitfalls

### Pitfall 22: Exit-code success hides verification scope

**What goes wrong:** Exit `0` can mean a valid segment, complete ledger, or signatures-only check.

**Warning signs:**

Human output is the only place that states scope.

**Prevention:**

Emit a machine-readable result with mode, bounds, head, keyring fingerprint, and completeness.

**Test evidence required:**

Golden CLI tests cover full, segment, signature-only, mismatch, and configuration-error output.

**Phase:**

R5.

**Confidence:** HIGH.

### Pitfall 23: NOTICE becomes a marketing or dependency dump

**What goes wrong:** Unrequired text confuses ownership and license obligations.

**Warning signs:**

NOTICE repeats the license, lists every dependency, or contains product claims.

**Prevention:**

Include only required attribution notices and approved project attribution.

**Test evidence required:**

Release review compares NOTICE against actual source and dependency requirements.

**Phase:**

R1.

**Confidence:** HIGH.

### Pitfall 24: Documentation keeps obsolete architecture claims

**What goes wrong:** README diagrams say rate limiting and audit logging are wired when runtime code differs.

**Warning signs:**

Docs describe planned components as shipped behavior.

**Prevention:**

Trace every launch claim to the production constructor and an acceptance test.

**Test evidence required:**

A production-style test exercises auth, limits, receipts, persistence, export, and verification.

**Phase:**

R9 and R10.

**Confidence:** HIGH.

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Required gate |
|---|---|---|
| R1 Apache relicense | Header replacement without ownership proof | Written authority, provenance inventory, release license audit |
| R2 Receipt format | Ambiguous bytes or privacy-heavy fields | Versioned format, domain tags, external vectors, privacy review |
| R3 Signing and rotation | Self-supplied keys or lost history | Pinned keyring, persistent signer, rotation continuity, restart tests |
| R4 Request path | False principal, missed denials, split transactions | Scoped auth, attempt state machine, serialized writes, crash tests |
| R5 Offline verifier | Shared bugs, loose parsing, false completeness | Independent vectors, strict parser, no-network test, expected head |
| R6 Export | Partial valid artifacts and metadata leakage | Signed manifest, snapshot export, authorization, range limits |
| R7 Delegation | Chain splicing and draft overclaim | Verified Biscuit lineage, token digest, splice tests, draft qualifiers |
| R8 Google connector | Demo action exceeds OAuth scope | Least-scope fixture and connector denial tests |
| R9 Quickstart | Ephemeral keys or hidden prerequisites | Cold timing, restart continuity, shipped verifier |
| R10 Comparison | Unsupported negative or absolute claims | Dated first-party claim ledger and launch-day refresh |
| R11 Contributors | DCO confusion and privacy surprise | Clear DCO wording, CI enforcement, scoped issue criteria |

## Required Launch Evidence

1. A pinned keyring verifies a clean export with networking disabled.
2. Cross-language vectors prove canonical encoding agreement.
3. Crash injection proves the durable attempt state machine.
4. Concurrent and restart tests prove one continuous sequence.
5. Tail deletion fails against a previously trusted head.
6. Every documented denial path has an explicit receipt policy.
7. Export tests cover snapshots, ranges, authorization, limits, and privacy canaries.
8. Delegation tests reject chain splicing under another parent and root.
9. Relicensing evidence and dependency notices are approved before contributions.
10. Every launch comparison cell has a dated first-party source.
11. A cold quickstart survives a container restart.
12. Final copy uses the qualified claims in this document.

## Research Gaps

- Copyright authority needs a written owner representation or legal review.
- The final receipt wire format does not exist yet.
- The durable attempt state machine needs phase-specific R4 design work.
- Biscuit policy facts need phase-specific R7 fixtures.
- External checkpoints remain out of scope, so completeness claims stay limited.
- Competitor capabilities require a fresh review before publication.

## Sources

### Local Evidence

- [Project milestone](../PROJECT.md)
- [Receipts and OSS PRD](../../PRD-receipts-oss.md)
- [Gateway request path](../../internal/gateway/gateway.go)
- [Current audit logger](../../internal/audit/logger.go)
- [Scoped key store](../../internal/auth/keys.go)
- [Authentication middleware](../../internal/auth/middleware.go)
- [SQLite setup](../../internal/db/sqlite.go)
- [Production command](../../cmd/agentgw/main.go)
- [Docker Compose](../../docker-compose.yaml)
- [Reference chain format](../../../agentic-operator-core/pkg/audit/chain.go)

### Official External Sources

- RFC 8032, Ed25519 contexts: https://www.rfc-editor.org/rfc/rfc8032.html
- Go `crypto/ed25519`: https://pkg.go.dev/crypto/ed25519
- RFC 8785, JSON canonicalization: https://www.rfc-editor.org/rfc/rfc8785.html
- Go JSON behavior: https://pkg.go.dev/encoding/json
- SQLite isolation: https://www.sqlite.org/isolation.html
- SQLite transactions: https://www.sqlite.org/lang_transaction.html
- SQLite WAL behavior: https://www.sqlite.org/wal.html
- Go transaction guidance: https://go.dev/doc/database/execute-transactions
- RFC 9162, signed heads: https://www.rfc-editor.org/rfc/rfc9162.html
- OWASP logging guidance: https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html
- European Commission data guidance: https://commission.europa.eu/law/law-topic/data-protection/data-protection-explained_en
- ICO pseudonymization guidance: https://ico.org.uk/for-organisations/uk-gdpr-guidance-and-resources/data-sharing/anonymisation/pseudonymisation/
- Apache license application: https://www.apache.org/legal/apply-license.html
- Apache provenance FAQ: https://www.apache.org/foundation/license-faq.html
- Developer Certificate of Origin: https://developercertificate.org/
- AAT draft `-00`: https://www.ietf.org/archive/id/draft-niyikiza-oauth-attenuating-agent-tokens-00.html
- OBO draft `-02`: https://www.ietf.org/archive/id/draft-oauth-ai-agents-on-behalf-of-user-02.html
- Biscuit Go repository: https://github.com/eclipse-biscuit/biscuit-go
- Biscuit Go module: https://raw.githubusercontent.com/eclipse-biscuit/biscuit-go/main/go.mod
- Biscuit Go security policy: https://raw.githubusercontent.com/eclipse-biscuit/biscuit-go/main/SECURITY.md
- FTC advertising basics: https://www.ftc.gov/business-guidance/advertising-marketing/advertising-marketing-basics
- FTC comparative policy: https://www.ftc.gov/legal-library/browse/statement-policy-regarding-comparative-advertising

All official sources were checked on 2026-08-11.
