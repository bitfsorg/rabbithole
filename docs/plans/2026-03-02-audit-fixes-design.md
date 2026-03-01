# Audit Fixes Design — 37 Remaining Issues

**Date**: 2026-03-02
**Scope**: 37 issues (3 HIGH + 34 MEDIUM) from code review `docs/audits/2026-03-01-review-summary.md`
**Excludes**: Pre-release blockers (B-01/B-02/B-03), already-fixed CRITICAL/HIGH issues

## Strategy

Execute the 10-task plan from `docs/plans/2026-03-01-comprehensive-audit-fixes.md`.

- **Branches**: `fix/audit-fixes` on both libbitfs-go and bitfs
- **Order**: libbitfs-go first (Tasks 1-7), then bitfs (Tasks 8-10) — bitfs depends on libbitfs-go
- **Verification**: All tests pass with `-race` on both repos

## Task Summary

| Task | Repo | Package | Issues | Priority |
|------|------|---------|--------|----------|
| 1 | libbitfs-go | tx | R05-H1/H3/M3/M4 | HIGH+MEDIUM |
| 2 | libbitfs-go | method42 | R01-M1/M2/M3/M4/M5/M6 | MEDIUM |
| 3 | libbitfs-go | x402 | R02-M1/M2 | MEDIUM |
| 4 | libbitfs-go | vault | R11-H3 | HIGH |
| 5 | libbitfs-go | vault | R11-M1/M3/M4/M5/M6, R04-M5 | MEDIUM |
| 6 | libbitfs-go | paymail | R09-H1/M2 | HIGH+MEDIUM |
| 7 | libbitfs-go | network+client | R08-M1/M2, R14-M1/M2/M3 | MEDIUM |
| 8 | bitfs | daemon | R12-M3/M4/M5 | MEDIUM |
| 9 | bitfs | cmd | R15-H2/H3 | HIGH+MEDIUM |
| 10 | bitfs | protocol | R07-M2/M3/M4, R03-M1/M2, R06-M1/M4/M5 | MEDIUM |

## Implementation Details

See `docs/plans/2026-03-01-comprehensive-audit-fixes.md` for per-task implementation guidance.
