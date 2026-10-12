# PLC Reader

PLC Reader is a planned standalone Go application that periodically reads word
regions from multiple PLCs, converts them into sensor measurements, and exports
them to an OpenTelemetry Collector over OTLP.

The repository is currently initialized for development. The production PLC
protocol and the final OTLP representation are intentionally not implemented
until the open design decisions are resolved.

## Requirements

- Go 1.27 or later in the Go 1 compatibility series
- GNU Make

No physical PLC or external Collector is required for the normal test suite.

## Development commands

```sh
make fmt
make test
make race
make vet
make build
make check
```

`make check` is the CI and completion gate. The built binary is written to
`bin/plc-reader`.

The current executable only provides the Phase 0 CLI scaffold:

```sh
go run ./cmd/plc-reader -version
```

Running it without options exits with a not-implemented error until the
application lifecycle is added in a later phase.

## Project documents

- `AGENTS.md`: contributor and coding-agent working agreement
- `docs/status.md`: current phase, next work, and blockers
- `docs/plc-reader-prototype.md`: initial requirements and tentative design
- `docs/implementation-plan.md`: phased implementation plan
- `docs/adr/`: accepted architectural decisions and the ADR template

Before implementation work, read `AGENTS.md` and `docs/status.md`. Significant
decisions should be recorded as ADRs rather than being left implicit in code.

## Test review policy

Every behavioral change must include appropriate tests. Tests created or modified
by an agent must pass, but they also require human review before their asserted
behavior is considered accepted. The agent's handoff must identify each affected
test file and summarize what it asserts.

## Contribution workflow

Work on a focused branch and run `make check` before committing. Concurrency-related
changes must also pass `make race`. A pull request should describe the change,
verification commands, unresolved decisions, and any agent-authored tests requiring
human review. Pull requests are not merged automatically.
