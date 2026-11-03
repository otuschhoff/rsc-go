# Changelog

## v0.5.0 - 2026-11-03

- Add bounded `ReadActiveBackupActivityObservations` for exact-scoped backup
  series candidates in explicit running/queued/canceling latest-activity states,
  without time cutoffs or requiring an end time. Reuse the existing SDK reader,
  immutable nullable observations, fingerprints and no-partial-result safeguards.
- Expose copied source/output scopes, status selection, row/page/byte counts and
  non-atomic completeness metadata; keep scheduled execution/root identity,
  whole-backup status/history and transfer-counter/duration guarantees unsupported.
- Preserve mandatory UTC windows for existing activity reads and all v0.4.0
  public structs, query selections and source semantics. Add no enrichment,
  credentials, caches, polling, persistence or consumer/deployment changes.
- Extend fixtures for old/no-end candidates, concurrency, null/zero bytes,
  scope/status mismatches, limits/cursors, denied reads, cancellation/deadlines,
  retry-like decreases/replay, unchanged cross-API fingerprints, and late child
  terminal-boundary corrections without backup-root promotion.
- Document how active candidates, windowed updates, known taskchain lifecycle and
  task reports differ, with source evidence, capability matrix and remaining
  whole-execution verification steps. Live tenant support remains unverified.

## v0.4.0 - 2026-11-03

- Add bounded SDK-backed Protection task-report reads with explicit cluster
  UUID scope, optional local workload selection, exact raw task/status selection,
  and exclusive task-end-time windows. Scan budgets include filtered-out rows;
  incomplete pages, duplicate row keys, malformed identities and exceeded limits
  return no partial observations.
- Preserve immutable nullable task report IDs, workload/cluster bindings, raw
  task status, start/end boundaries, unscaled source duration, separate logical/
  physical bytes and reported task-total transferred bytes, with source-only
  revision fingerprints and explicit source/output coverage metadata.
- Add separate task-report capabilities and transferred-byte contracts without
  changing v0.3.0 API semantics: task totals are not cumulative execution counters,
  and task boundary intervals are not whole-job or active transfer durations.
- Document evidence, consumer publication limits and concrete verification steps
  for unavailable scheduled-execution/root/series bindings, retry grouping,
  whole-execution lifecycle, counter mode/compression and transfer-phase metrics.
- Extend SDK-backed fixtures for exact filters, concurrent task rows, late
  corrections, reset-like decreases, null/zero counters, unsupported semantics,
  denied access, incomplete enumeration and bounded request/resource failures.
  Live tenant support remains unverified; polling/persistence remain consumer-owned.

## v0.3.0 - 2026-11-03

- Add bounded, exact workload/cluster-scoped backup activity observations using
  explicit UTC start/update windows and one SDK attempt per page. Reject
  incomplete/duplicate/out-of-scope results, cursor cycles, and exceeded bounds
  without silent truncation or partial observations.
- Preserve nullable source times, event-associated transferred bytes, logical
  bytes, raw latest-activity states, and related-job attempt metadata in immutable
  observations with replay fingerprints and explicit collection/completeness data.
- Add bounded lookup of caller-known job-instance taskchain UUIDs with documented
  terminal-state mapping, nullable start/end boundaries, and valid terminal
  taskchain elapsed duration; no inferred workload association or discovery.
- Add sanitized monitoring errors preserving cancellation/deadline/authority
  identity, and fixture coverage for source states/windows, retries/reset-like
  changes, late revisions, response/read bounds, paging, denied access, and replay.
- Document unsupported scheduled-execution identity/join, whole-run series
  status/completion, counter mode/compression, retry grouping, and transfer-phase
  duration. Legacy APIs/enrichment are unchanged and are not upgraded into
  authoritative execution history; polling/persistence remain consumer-owned.

## v0.2.0 - 2026-11-03

- Select CDM snapshot local, archival, and replication locations; retain location
  IDs/names and active metadata, and report combined storage copies accurately.
- Reject incomplete workload snapshot pagination and stalled/cyclic cursors
  without returning partial lists.
- Add paged snapshot file browsing with paths, file modes, sizes, timestamps,
  status messages, and optional prefix-search controls.
- Add bounded recursive snapshot file manifests with directory pagination,
  symlink preservation without traversal, and no partial results on failure.
- Document that the public browse schema does not expose POSIX inode numbers,
  hard-link identity, incremental-copy history, or proof of recoverability.

## v0.1.2 - 2026-11-03

- Add paged backup job listing with workload/cluster/status filters, progress,
  activity-series cancellation IDs, and presence-aware cancelability metadata.
- Add on-demand backup initiation with per-workload taskchain IDs and errors;
  preserve partial successes and reject missing or inconsistent outcomes.
- Add separate activity-series and taskchain cancellation methods with explicit
  acknowledgement handling, one SDK attempt, and no automatic mutation replay.
- Test lifecycle payloads, partial starts, invalid inputs, incomplete pages,
  rejected cancellations, GraphQL/HTTP failures, and canceled contexts.
- Add reusable NAS primary-fileset inventory with share/workload IDs and
  configured, effective and pending SLA references.
- Correct NAS SLA assignments to use primary-fileset configured/effective
  matches, preserving explicit unprotection, unknown state and policy identity.
- Allow a complete caller-owned inventory to be reused across SLA policies.
- Reject incomplete NAS responses and stalled/cyclic pagination without partial
  results; remove duplicate NAS reporting/template assignments.

## v0.1.1 - 2026-11-03

- Bound node-stat reads to a one-minute deadline, 4 MiB per response,
  100 clusters per page, and 4096-byte cursors without changing SDK authentication.
- Add caller-selectable `MaxPages` (1-10) and `MaxSamples` (1-10000), with
  bounded defaults and no partial results when a limit is exceeded.
- Reject missing connections, nodes or pagination status and test response,
  page and sample limits alongside existing node and inventory fixtures.

## v0.1.0 - 2026-11-03

First tagged minor release, following the initial pseudo-version publications.

### Added

- Per-member performance reads through `Client.NodeStats` and
  `FetchNodeStatsForCluster`, backed by RSC's `Cluster.clusterNodeStats`.
- Timestamped CPU utilization, RAM-used percentage, read/write IOPS, and
  read/write throughput, grouped by cluster and identified by member node ID.
- Optional time ranges, `AVERAGE`/`MAX`/`UNDEFINED` aggregation, node filtering,
  and cluster pagination with nonadvancing/cyclic cursor protection.
- Opt-in disk utilization in basis points and five-minute load average in
  thousandths for tenant schemas exposing those newer fields.
- Presence-aware metric pointers that distinguish unavailable values from zero.
- Regression tests for query variables, UTC ranges, pagination, decoding,
  missing metrics, invalid options, GraphQL errors, and HTTP failures.

### Included From Earlier Publications

- Reusable cluster inventory, health/capacity, SLA, fileset/NAS, snapshot, and
  backup activity APIs, with stable configuration serialization.
- Application-owned authorized HTTP clients and standalone in-memory SDK
  service-account authentication, without library-managed credential persistence.
- Bounded, presence-aware fileset inventory reads for monitoring.
- Published dependency instructions and the narrowly extended Polaris SDK fork.

### Compatibility And Validation

- Existing health, configuration, and authorization behavior is unchanged.
- CPU utilization retains the raw API scale; the public schema does not specify
  its units. RAM usage is a percentage; IOPS are operations/second and throughput
  is bytes/second. Samples are RSC observations, not live member polls.
- Unsupported operations or requested fields return errors without silent
  fallback or fabricated values. Live-tenant support remains unverified.
- Consumers still require the explicit SDK replacement documented in README.