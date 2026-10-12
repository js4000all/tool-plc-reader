# Development Status

## Current state

- Phase 0 development scaffold is present.
- The standard completion gate is `make check`.
- The normal test suite must not require a physical PLC, external Collector, or
  public network access.
- `docs/implementation-plan.md` is the design baseline, subject to accepted ADRs
  and the unresolved decisions listed in its section 11.
- The Phase 0 CLI tests in `cmd/plc-reader/main_test.go` were reviewed and accepted
  by the project owner on 2026-10-12.

## Next phase

Phase 1: domain types and `RegionSnapshot` decoders.

The next implementation task should define and test IDs, addresses, snapshots,
measurements, values, and bit/U16/S16/U32 decoding without introducing network or
OTLP dependencies.

## Blockers and decision boundaries

- Phase 6 production PLC work is blocked until the PLC protocol and wire details
  are confirmed.
- Phase 7 final OTLP adapter work is blocked until the signal and schema contract
  is confirmed.
- Other unresolved product decisions are listed in section 11 of
  `docs/implementation-plan.md` and must not be silently fixed in code.

## Definition of done

- Relevant tests have been added or updated and pass.
- `make check` passes.
- `make race` passes when concurrency is affected.
- Documentation and ADRs reflect behavior and significant decisions.
- Any agent-authored tests are explicitly listed for human review in the handoff.
