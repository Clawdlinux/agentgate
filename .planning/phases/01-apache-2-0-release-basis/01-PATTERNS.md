# Phase 1: Apache 2.0 Release Basis - Pattern Map

**Mapped:** 2026-08-12
**Files analyzed:** 8 new or modified files
**Analogs found:** 7 / 8

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `LICENSE` | release config | file-I/O copy | `../agentic-operator-core/LICENSE` | exact |
| `NOTICE` | release config | file-I/O static text | No local NOTICE exists | no analog |
| `docs/relicense-authorization.md` | governance record | file-I/O and manual approval | `01-RESEARCH.md` lines 145-160 | specification match |
| `README.md` | documentation | text transform | Existing `README.md` lines 211-213 | exact in-place |
| `cmd/agentgw/main.go` | command source metadata | text transform | `../agentic-operator-core/pkg/audit/chain.go` lines 1-4 | exact header |
| `internal/gateway/gateway.go` | controller source metadata | text transform | `../agentic-operator-core/pkg/audit/chain.go` lines 1-4 | exact header |
| `internal/registry/registry.go` | service source metadata | text transform | `../agentic-operator-core/pkg/audit/chain.go` lines 1-4 | exact header |
| `internal/vault/vault.go` | service source metadata | text transform | `../agentic-operator-core/pkg/audit/chain.go` lines 1-4 | exact header |

## Pattern Assignments

### `LICENSE` (release config, file-I/O copy)

**Analog:** `../agentic-operator-core/LICENSE`

Canonical source: `https://www.apache.org/licenses/LICENSE-2.0.txt`.

Copy the reviewed sibling file byte for byte. It matches the canonical form and supports an offline check. Do not add project identity or comments.

**Opening pattern** (lines 1-4):

```text
Apache License
Version 2.0, January 2004

http://www.apache.org/licenses/
```

**Closing marker** (line 176):

```text
END OF TERMS AND CONDITIONS
```

**Required check:**

```bash
cmp -s LICENSE ../agentic-operator-core/LICENSE
```

### `NOTICE` (release config, file-I/O static text)

**Analog:** None. The sibling repository has no `NOTICE` file.

Use exactly the identity locked by `01-CONTEXT.md` line 23:

```text
AgentGate
Copyright 2026 Clawdlinux.
```

Do not add ASF language or dependency names. No current source artifact evidence requires them.

**Required check:**

```bash
test "$(cat NOTICE)" = $'AgentGate\nCopyright 2026 Clawdlinux.'
```

### `docs/relicense-authorization.md` (governance record, manual approval)

**Analog:** No existing repository document has this legal role.

Use the required shape from `01-RESEARCH.md` lines 145-160. Use PRD identity from `PRD-receipts-oss.md` lines 3-6.

```markdown
# AgentGate Relicense Authorization

- Repository: `github.com/Clawdlinux/agentgate`
- Reviewed revision: `<commit SHA or release revision>`
- Effective date: `<YYYY-MM-DD>`
- Owner granting permission: Shreyansh Sancheti

## Scope

Every distributed first-party file in the reviewed revision, including:

- `cmd/agentgw/main.go`
- `internal/gateway/gateway.go`
- `internal/registry/registry.go`
- `internal/vault/vault.go`

Third-party works remain under their own terms.

## Permission

The owner grants permission to release the scoped first-party work under Apache License 2.0.

## Owner Affirmation

Status: Pending personal review and affirmation.
Reviewed by: Shreyansh Sancheti
Review date: `<YYYY-MM-DD>`
```

Keep the status pending until Shreyansh personally affirms the record. State that Git authorship and sign-off support provenance but do not prove legal authority. Block merge on incomplete authority.

### `README.md` (documentation, text transform)

**Analog:** Existing `README.md` lines 211-213.

Preserve the existing heading, blank line, and single-sentence style:

```markdown
## License

AgentGate is licensed under the Apache License 2.0. See [LICENSE](LICENSE).
```

Do not add badges, patent summaries, legal interpretation, or launch copy.

### 4 locked Go files (source metadata, text transform)

**Files:**

```text
cmd/agentgw/main.go
internal/gateway/gateway.go
internal/registry/registry.go
internal/vault/vault.go
```

**Analog:** `../agentic-operator-core/pkg/audit/chain.go` lines 1-4.

Replace each current 7-line BSL block with this exact 4-line block:

```go
/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/
```

The blank line after the header remains. Do not change package comments, imports, formatting, or runtime code.

Current files all share this incompatible block at lines 1-6:

```go
/*
Copyright 2026 Clawdlinux.

Licensed under the Business Source License 1.1.
See LICENSE in the repository root.
*/
```

## Shared Patterns

### Scope Control

`01-CONTEXT.md` lines 27-28 lock the 4 Go paths and exact sibling header. No other Go file receives a header.

Use path equality and body comparisons. A header count alone is insufficient.

```bash
expected=$'cmd/agentgw/main.go\ninternal/gateway/gateway.go\ninternal/registry/registry.go\ninternal/vault/vault.go'
actual=$(git diff HEAD --name-only -- '*.go' | sort)
test "$actual" = "$expected"

while IFS= read -r file; do
  cmp -s \
    <(git show HEAD:"$file" | sed '1,7d') \
    <(sed '1,5d' "$file") || exit 1
done <<< "$expected"
```

The first check proves only the 4 locked Go files changed. The second proves content below each header is unchanged.

### Header State

```bash
actual=$(git ls-files '*.go' | xargs rg -l \
  'Licensed under the Apache License, Version 2\.0\.' | sort)
test "$actual" = "$expected"

if git ls-files '*.go' | xargs rg -n \
  'Business Source License|Business Source|BSL'; then
  exit 1
fi
```

These checks prove exact Apache header coverage and no BSL residue in tracked Go files.

### README Style

The README uses short `##` sections, a blank line, then direct prose. Keep that structure.

```bash
rg -n '^## License$' README.md
rg -n 'Apache License 2\.0.*\[LICENSE\]\(LICENSE\)' README.md
```

### Authority Boundary

Automation can prove text state. It cannot prove ownership or permission.

Apply these rules to the authority record and merge checklist:

- Name the repository, revision, effective date, owner, and first-party scope.
- List the 4 BSL-marked paths.
- Keep personal affirmation explicit and pending until reviewed.
- Never treat Git authorship or `Signed-off-by` as legal authorization.
- Block merge if authority is incomplete or uncertain.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `NOTICE` | release config | file-I/O static text | Neither repository has a NOTICE file. Use the locked 2-line identity. |
| `docs/relicense-authorization.md` | governance record | file-I/O and manual approval | No existing legal authority record exists. Follow the phase decisions and research shape. |

## Planner Checks

Run the focused release checks after each licensing task:

```bash
test -s docs/relicense-authorization.md
rg -n 'AgentGate|github.com/Clawdlinux/agentgate|Apache License 2\.0|Shreyansh Sancheti' \
  docs/relicense-authorization.md
cmp -s LICENSE ../agentic-operator-core/LICENSE
test "$(cat NOTICE)" = $'AgentGate\nCopyright 2026 Clawdlinux.'
rg -n '^## License$' README.md
rg -n 'Apache License 2\.0.*\[LICENSE\]\(LICENSE\)' README.md
go test ./cmd/agentgw ./internal/gateway ./internal/registry ./internal/vault
git diff --check
```

At phase completion, run `go test ./...` and the scope-control checks above. After commit, inspect the source archive for `LICENSE`, `NOTICE`, and the authority record.

The owner affirmation remains a manual merge gate.

## Metadata

**Analog search scope:** Phase inputs, root documentation, 4 target Go files, and sibling Apache release files.

**Files scanned:** 14 required files. The optional sibling `NOTICE` does not exist.

**Pattern extraction date:** 2026-08-12
