# Proposal: v2 stream digest

Status: proposed. Not implemented.

SSE requires a receipt commit before the first response byte.
The final digest isn't known at that point.
Today's v1 receipt records upstream acceptance. Server logs record the final digest and byte count.
Those logs aren't signed stream attestations.

Propose an optional v2 `stream_result` field on a signed completion artifact.
The artifact references the original receipt's `entry_hash`.
`stream_result` contains `sha256`, `bytes`, and `termination`.
The hash covers bytes accepted by downstream writes, including partial writes.
It doesn't prove client delivery.

Keep exactly 1 action receipt per attempt. The completion artifact isn't another action receipt.
An absent completion artifact must not imply successful completion.
Signature domains, crash recovery, and verifier behavior need spec review before implementation.
No v1 fields, encoding, or signatures change.
