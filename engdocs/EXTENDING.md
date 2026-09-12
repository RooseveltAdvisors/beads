# Extending bd

This file documents contracts that callers of the storage API must honor.
It is not user-facing; it is for code that embeds bd or talks to the
storage layer directly.

## Lite SELECT shape — `IssueFilter.Lite`

`store.SearchIssues(ctx, query, filter)` accepts an `IssueFilter` value.
When `filter.Lite == true`, the storage layer issues a narrower SELECT
that omits these heavy TEXT columns:

- `description`
- `design`
- `acceptance_criteria`
- `notes`
- `waiters`
- `payload`

### Contract for callers

Code that calls `store.SearchIssues` with `IssueFilter.Lite == true`:

- **MUST NOT** read `Description`, `Design`, `AcceptanceCriteria`,
  `Notes`, `Payload`, or `Waiters` from any returned `*types.Issue`.
  These fields are zero-valued after a lite scan; they did not come from
  the row. Reading them yields no signal.
- **MAY** read every other field — identity, status, priority,
  timestamps, labels, dependencies, metadata, etc. Lite preserves them.
- **MUST** detect lite-fetched records via `issue.IsLitePartial` if
  branching behavior on hydration depth is required. The field is
  internal-only (`json:"-"`) — it never crosses the wire.

To recover the full body for a specific issue after a lite listing,
call `store.GetIssue(ctx, id)` — `GetIssue` always returns the full row.

### Default behavior

`IssueFilter.Lite` defaults to `false`. Every existing call site that
does not opt in retains today's behavior: heavy columns are fully
hydrated, and `Issue.IsLitePartial` is `false`.

### Where the contract is enforced

- Column lists: `internal/storage/issueops/scan.go`
  (`IssueSelectColumns`, `IssueSelectColumnsLite`, `HeavyDropList`).
- Scan helpers: `ScanIssueFrom` (full) and `ScanIssueLiteFrom` (lite,
  sets `IsLitePartial`).
- SELECT dispatch: `internal/storage/issueops/search.go` — `SearchIssuesInTx`
  selects `issueProjection` or `issueLiteProjection` on `filter.Lite`; both
  are `searchProjection[*types.Issue]` literals sharing the wisp-merge and
  hydration machinery in `searchTableInTxT`.
- Schema-parity guard:
  `internal/storage/issueops/scan_test.go::TestIssueSelectColumns_LitePlusHeavyEqualsFull`
  fails CI if a future column is added to `IssueSelectColumns` without
  being classified into `IssueSelectColumnsLite` or `HeavyDropList`.

### Backend coverage

`filter.Lite` is currently honored only by the issueops-backed stores
(Dolt, embedded Dolt) via the dispatch above. The proxied-server
(`internal/storage/domain/db`) path — `issueSQLRepositoryImpl.searchTable`
/ `fetchIssuesByIDs` — does not check `filter.Lite` yet: it always issues
the full `issueSelectColumns` SELECT and returns fully-hydrated issues
with `IsLitePartial == false`. This is correct-but-unoptimized (no lite
callers exist yet, so the difference is invisible today); wiring
`filter.Lite` through the domain/db stack is deferred to the CLI-wiring
follow-up (be-uwvs.2+), not part of this foundation.

## Adding a column to the issue row shape

The issues row shape is projected positionally in several places and mirrored
onto `wisps`, so a new column is never one edit. The guard tests below fail
until every leg agrees, which is the intended way to find the list — but
knowing it up front turns a day of red CI into one pass. Migration 0067
(`repeat_pattern` / `repeat_start` / `repeat_end` / `due_source`) is the
worked example; 0060 (`storage_class`) is the earlier one.

**Schema.** A guarded main-plane migration adding the column to `issues` AND
`wisps`, a `migrations/ignored/` twin adding it to `wisps` alone (wisps is
dolt-ignored, so fresh clones never run the main migration — see
`scripts/check-migration-hygiene.sh` check D), and a direct-DDL override in
`internal/storage/schema/cli_migrations.go` (the Dolt CLI batch path no-ops
prepared ALTERs). If the override drops a `@has_wisps` guard, list the
migration in `cliSubstituteAssumesWispTables`.

**Model.** The field on `types.Issue`; a clone in `cloneIssueForHook`
(`internal/storage/hook_decorator.go`) and in `clonePublicIssue`
(`internal/storage/issueops/public_snapshot.go`) if it is a pointer or slice;
an alias in `backend/types.go` if it introduces a new named type.

**Projections.** `sqlbuild.IssueBaseColumns` and `IssueBaseColumnsLite`; both
scanners in `internal/storage/issueops/scan.go`; both INSERTs
(`issueops/helpers.go` and `internal/storage/domain/db/issue.go`), including
their `ON DUPLICATE KEY UPDATE` lists if the column should survive re-import;
`loadIssuesProjection` in `internal/migration/legacysqlite/reader.go` (a NULL
placeholder — the legacy schema has no such column).

**Write paths.** `issueops.IsAllowedUpdateField` plus the `matches*` case
beside it, `domain/db`'s `allowedUpdateFields` (and `timestampUpdateFields`
for a DATETIME), `publicops.IssuePatch` and `issueops.UpdateFields`,
`dolt.nonCoordinationPatchSignals`, and `publicCreateIssue`.

**Wire.** All five read schemas in `internal/httpapi/spec/openapi.v0.yaml`
(`Issue`, `IssueWithCounts`, `IssueDetails`, `IssueWithDependencyMetadata`,
`TreeNode`) or `TestWireTagBijection` fails; the request schemas
(`CreateIssueRequest`, `IssuePatchBody`, `ApplyCreateItem`, `ApplyPatchBody`)
if callers may set it, then `make api-gen`; the member allowlists and wire
mappings in `internal/httpapi/{create,update,batch_apply}.go`.

**CLI.** Flags on `bd create` and `bd update`, the `singleIssueOnlyFlags` list
in `cmd/bd/create_input.go`, `createIssueParams` and `buildCreateIssue`, the
proxied builder in `cmd/bd/create_proxied_server.go`, and `GraphApplyNode` in
`cmd/bd/graph_apply.go` (`TestGraphApplyNodeCoversCreateIssueParams` enforces
parity between `bd create` and graph plans).

**Fixtures with hardcoded arity.** `internal/storage/domain/db/issue_scan_parity_test.go`
and `internal/storage/issueops/search_lite_merge_test.go` both pin a column
count and a full-width row.

**A SYSTEM-MAINTAINED column skips most of this.** If one subsystem is the
column's only writer and no caller may set it — migration 0068 (`due_missed`,
written only by the due sweep) is the worked example — then the Write paths,
Wire request schemas and CLI sections above do not apply at all: no `bd create`
or `bd update` flag, no `GraphApplyNode` entry, no `IsAllowedUpdateField` or
`allowedUpdateFields` member, no `CreateIssueRequest`. What remains is Schema,
Model, Projections, the five wire READ schemas, and the arity fixtures. Two
things still need a decision rather than a default: classify the field in
`TestPublicCreateIssueFieldClassificationIsComplete` (`ignored`, with the
reason a creator must not be able to declare it), and decide deliberately
whether it belongs in the domain INSERT's `ON DUPLICATE KEY UPDATE` list —
omitting it means a re-import preserves the value this workspace observed
rather than adopting the incoming one, which is usually what observed state
wants.
