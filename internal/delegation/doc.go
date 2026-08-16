/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Package delegation binds attenuated Biscuit tokens to verified AgentGate
// requests and receipt lineage (DELG-01 through DELG-06).
//
// AgentGate does not define a new delegation token format. It uses Biscuit
// (github.com/biscuit-auth/biscuit-go), an existing, deployed attenuated-
// authority token format, as design context for two IETF drafts discussing
// the same problem: an agent acting for a human needs authority narrower
// than the human's own, and that authority should be independently
// verifiable and revocable-by-non-issuance:
//
//   - draft-niyikiza-oauth-attenuating-agent-tokens
//   - draft-oauth-ai-agents-on-behalf-of-user-02
//
// Neither draft defines a wire format Biscuit implements, and this package
// makes no standards-compliance claim against either one. They are cited
// here as the design problem this feature addresses, per PRD-receipts-oss.md
// TASK-R7.
package delegation
