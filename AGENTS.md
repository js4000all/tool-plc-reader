# Development Guidelines

## Required Reading and Sources of Truth

Read these files before changing implementation or architecture:

1. `AGENTS.md` for working rules.
2. `docs/status.md` for the current phase and blockers.
3. `docs/plc-reader-prototype.md` for product requirements.
4. `docs/implementation-plan.md` for the proposed architecture and phased plan.
5. Accepted records under `docs/adr/` for finalized architectural decisions.

When documents conflict, an accepted ADR takes precedence over the implementation
plan, and the implementation plan takes precedence over tentative proposals in the
prototype document. Do not treat tentative proposals as finalized requirements.

## Engineering Principles

- Implement the PLC Reader in Go.
- Prefer simple, idiomatic Go code and avoid unnecessary abstractions.
- Keep PLC communication separate from data processing.
- Keep queues and retries bounded.
- Do not make normal tests depend on a physical PLC, an external Collector, or
  public network access.
- Add or update tests for every behavioral change.

## Working Agreement

- Work only within the phase and task currently requested. Keep changes small and
  preserve unrelated tracked and untracked changes.
- Resolve parsing, references, and validation at startup rather than repeatedly in
  periodic collection paths.
- Define interfaces at the consumer boundary and only when a real substitution or
  test boundary exists.
- Do not add a production dependency without documenting why the standard library
  or an existing dependency is insufficient.
- Do not edit generated files by hand. Do not commit credentials, real PLC
  addresses, tokens, or private Collector endpoints.
- Record significant or difficult-to-reverse decisions as ADRs using
  `docs/adr/template.md`.

## Mandatory Stop Conditions

Stop implementation and request a decision instead of inventing a specification
when work depends on any unresolved item in section 11 of
`docs/implementation-plan.md`. In particular:

- Do not implement the production PLC wire protocol until its protocol, frame,
  address, byte/word order, and read limits are confirmed.
- Do not finalize the OTLP adapter until the signal type, schema, attributes,
  security, and delivery expectations are confirmed.
- Do not silently choose data-loss, persistence, or partial-startup behavior.

Code that depends only on fake interfaces may proceed when the current phase allows
it, but it must not encode a tentative wire protocol or telemetry schema as final.

## Standard Commands and Completion

Use the repository commands rather than ad hoc alternatives:

- `make fmt` formats Go code.
- `make test` runs the normal test suite.
- `make race` runs tests with the race detector.
- `make vet` runs static checks.
- `make build` builds the binary.
- `make check` runs the non-mutating completion checks used by CI.

An implementation task is technically complete only when its relevant tests pass
and `make check` succeeds. Run `make race` for concurrency-related changes.

Tests added or modified by an agent require human review. The agent must list those
test files and the behavior they assert in its handoff. Passing tests establish the
technical completion gate but do not imply that newly added tests have been
accepted by the reviewer.

## Git and Pull Request Workflow

- Inspect `git status` before editing, before committing, and before handoff.
- Never commit directly to the default branch. Use a focused branch for one task.
- Do not discard, overwrite, commit, or reformat unrelated user changes.
- Commit only a coherent change that passes the required completion checks. Use a
  short imperative commit subject consistent with the repository history.
- Do not rewrite published history, force-push, or bypass repository protections.
- When the task is complete and no product or architecture decision requires a
  human answer, an agent may commit, push its branch, and open or update a pull
  request without asking for separate confirmation.
- A pull request may be opened to obtain the required review of agent-authored
  tests. Its description must list every added or modified test file and summarize
  the behavior asserted. Do not represent those tests as accepted until reviewed.
- Include the commands and results used for verification in the pull request.
- Do not merge a pull request, delete a remote branch, create a release, or tag a
  release unless the user explicitly requests that action.
