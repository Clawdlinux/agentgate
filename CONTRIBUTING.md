# Contributing to AgentGate

Thanks for considering a contribution. AgentGate is Apache License 2.0
and welcomes outside pull requests.

## Prerequisites

- **Go 1.25.0** or later.
- **Docker** (optional) — only needed to run the full quickstart described
  in `README.md`; not required to build, test, or contribute code.

No C compiler, database, API keys, or secrets are needed to build or run
`make test`. The SQLite driver is pure Go. Every test either uses an
in-memory/temp-file SQLite database or a local `httptest` server. The
optional `-race` check below does require a C compiler.

## Build, test, and lint

```bash
make build          # builds bin/agentgate (the gateway)
make build-verify   # builds bin/agentgate-verify (the offline verifier CLI)
make test           # go test -v -count=1 ./...
make lint           # golangci-lint run ./... (install separately: https://golangci-lint.run/welcome/install/)
```

Before opening a pull request, at minimum run:

```bash
go build ./...
go vet ./...
gofmt -l .           # must produce no output
go test ./... -race -count=1
```

This repository has no CI workflow configured yet, so these are the same
checks a reviewer will run locally against your branch — running them
yourself first saves a review round trip.

## Making a focused pull request

- **One logical change per pull request.** A starter issue (see below)
  should map to one PR. Do not bundle an unrelated refactor, dependency
  bump, or formatting pass into the same PR as a feature or fix.
- **Branch off `main`**, never commit directly to it.
- **Write a clear PR description**: what changed, why, and how you
  verified it (paste the relevant `go test` output or describe the manual
  check you ran).
- **Keep the diff reviewable.** If a change grows well beyond what its
  issue described, that is a signal to split it into more than one PR
  rather than one large one.

## Signing off your commits (DCO)

Every commit must carry a **Developer Certificate of Origin (DCO)**
sign-off line, added automatically with:

```bash
git commit -s -m "your commit message"
```

This appends a line like:

```
Signed-off-by: Your Name <you@example.com>
```

**This is not a cryptographic signature.** `git commit -s` only asserts —
under your own name and git-configured email, which anyone can set to
anything — that you have the right to submit the change under this
project's license (the standard [DCO](https://developercertificate.org/)
text). It does not use GPG, SSH, or any other key, and it does not prove
authorship the way a cryptographically signed commit
(`git commit -S`) does. AgentGate's own commits use `-s`, not `-S` — do
not confuse the two, and do not describe sign-off as "signing" in a way
that implies cryptographic verification.

A pull request whose commits are missing a sign-off line will need to be
amended (`git commit --amend -s`, or an interactive rebase adding `-s` to
each commit) before it can be merged.

## Finding something to work on

Issues labeled [`good first issue`](https://github.com/Clawdlinux/agentgate/labels/good%20first%20issue)
are scoped to be independently testable without needing repository
maintainer context, and without any secrets or live third-party API
credentials. Each one names the files it touches, the acceptance checks a
reviewer will look for, and a test path you can run locally to confirm
your change before opening a PR.

## Code of conduct

Be respectful and constructive in issues, pull requests, and reviews.
Disagree about code and design, not about people.
