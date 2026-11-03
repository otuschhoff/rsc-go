# rsc-go

Reusable Rubrik Security Cloud inventory, monitoring, SLA, NAS/fileset and
snapshot routines, extracted from opsctl. Module:
`github.com/otuschhoff/rsc-go`; package name: `rsc`.

Current release: `v0.3.0`. See [CHANGELOG.md](CHANGELOG.md) for release notes.

The official Rubrik Polaris SDK v1.11.0 executes **every GraphQL request**.
Its service-account token source and transport also provide standalone
authentication. Custom query selections are retained where prepared SDK
operations omit required fields, identities or pagination. This is not a
replacement implementation of SDK authentication.

## Application-owned authorization

An application with a central credential authority supplies its authorized HTTP
client. The library does not add authentication, resolve credentials, read
credential files, discover environment credentials or write application caches.

```go
import rsc "github.com/otuschhoff/rsc-go"

client, err := rsc.NewClient(graphqlEndpoint, authorizedHTTPClient)
if err != nil {
    return err
}
clusters, err := client.Clusters(ctx, "")
```

In opsctl, the supplied transport acquires and authorizes a credential lease for
each request. Reusing an rsc-go client does not bypass changes to policy, locks,
revocation or the credential HTTP client's bound lifetime.

## Standalone in-memory service account

The caller supplies credentials already held in memory:

```go
client, err := rsc.NewServiceAccount(rsc.Credentials{
    Name:           "monitor",
    ClientID:       clientID,
    ClientSecret:   clientSecret,
    AccessTokenURI: "https://tenant.my.rubrik.com/api/client_token",
}, "") // derive GraphQL URL from the token endpoint
if err != nil {
    return err
}
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
health, err := client.Health(ctx, "")
```

`NewServiceAccountJSON(content, endpoint)` accepts the same service-account JSON
as bytes, **not a filename**. Neither constructor contacts the network. The SDK
acquires a token on the first request and refreshes/reuses it in memory, including
across concurrent requests. Disk token caching and credential environment
overrides are not enabled. Credentials remain in Go memory for the client's
lifetime; strings and SDK copies cannot be reliably zeroized.

Production token/API endpoints require HTTPS. HTTP is accepted only for
loopback fixtures. Redirects are blocked on both token and GraphQL clients.
Callers must supply contexts with appropriate deadlines. Authorization, token
refresh ownership, storage, lifecycle and retry policy should not be split
between a central-authority client and a standalone client for the same
application.

## API and persistence boundaries

Typed client methods cover clusters, health/capacity, SLA lists/details and
assignments, filesets/templates, NAS objects/shares/sources, target mappings,
workload snapshots, backup activities/jobs, on-demand backup initiation,
cancellation, and snapshot/activity enrichment.
Package-level functions also accept an authorized HTTP client and exact endpoint.
`GraphQL` is the explicit custom-operation escape hatch; it decodes the complete
GraphQL envelope, not just `data`.

Existing field selections and cluster/global SLA distinctions are preserved.
SLA-related reads retain bounded three-attempt transient HTTP/transport retries;
other reads and custom GraphQL calls make one attempt. SDK internal retry
classification is not used: a JSON `message` body must not turn a retryable 503
into an immediate permanent failure. Errors retain HTTP status and wrapped
transport identity via `RequestError`, including `errors.Is` for authority denial
and cancellation. Protocol failures are explicit: the SDK requires a JSON body
with Content-Type `application/json` and HTTP status 200.

`ConfigDump` and conversion helpers retain the existing YAML/cache field
contracts. `SerializeConfigYAML` applies `StableConfig(dump)` automatically to
exclude volatile timestamps, space usage and health observations from configuration.
Persist observations separately under the consuming application's policy.
The library does not save files, create directories, discover CDOT objects,
manage Git, or maintain a singleton/global cache. Per-request user-agent and
logging hooks are available through `WithRequestOptions`.

## Snapshot locations and contents

`Client.WorkloadSnapshots(ctx, workloadID)` / `FetchSnapshotsForWorkload` list
snapshots with CDM local, archival, and replication locations. Each location
includes its ID, name, cluster UUID, type, and nullable active flag. The existing
`Snapshots` rows also retain archive names and derive their storage labels from
these selections. `DetermineSnapshotStorage` preserves `rubrik` for local,
`archiv` for archive, `replica` for replication, and combined labels for multiple
copies; unavailable location information is `unknown` for typed snapshots.
Migration states remain explicit. Missing page metadata and nonadvancing/cyclic
cursors fail without partial workload lists. Location metadata, including an
inactive location, does not by itself certify present restore availability.

For one snapshot, `Client.SnapshotFiles(ctx, snapshotID, SnapshotFilesOptions)` /
`FetchSnapshotFiles` browse the indexed files and folders through
`browseSnapshotFileConnection`. Use the persistent RSC snapshot UUID from the
listing, not a CDM snapshot ID. `Path` defaults to `/`; supply the API's path
for other roots. Paths are passed verbatim, including Windows paths.
Each call returns one page of entries with absolute/display/relative paths,
filename, file mode, size, nullable modification time, and status message.
`First` is 1-100 (default 100); continue using `EndCursor` as `After` while
`HasNextPage` is true. Optional prefix-search settings are passed to the API.

For a recursive, unfiltered subtree listing:

```go
manifest, err := client.SnapshotFileManifest(ctx, snapshotID, "/", rsc.SnapshotManifestLimits{
    MaxPages:   100,
    MaxEntries: 10000,
})
```

`SnapshotFileManifest` / `FetchSnapshotFileManifest` traverse directories and
all their pages, retaining symlinks without following them. Limits are global
across the subtree: 1-100 pages and 1-10000 entries, with those maxima as
defaults. Both browse and manifest reads have a maximum one-minute deadline,
one SDK attempt per page, 4 MiB per response, and 4096-byte cursors. Shorter
caller deadlines remain authoritative. Incomplete responses, cycles, duplicate
paths, unsupported traversal modes, exceeded bounds, or any page failure
return no partial manifest. Successful empty listings have non-nil entry slices.

The public RSC `SnapshotFile` and fileset browse schemas do **not** expose POSIX
inode numbers or hard-link identity. These APIs enumerate logical indexed
snapshot members by path; they do not invent inode IDs, identify which bytes
were newly copied by an incremental backup, or certify recoverability. Preserve
and inspect entry status messages. Indexing, workload support, permissions, and
archive browse availability remain tenant-dependent; errors are not converted
to empty listings. No archive hydration, restore/mount, or live-file-system scan
is initiated. Live-tenant behavior remains unverified.

Schema contracts: [CdmSnapshot](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/objects/CdmSnapshot/),
[browseSnapshotFileConnection](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/queries/browseSnapshotFileConnection/),
and [SnapshotFile](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/objects/SnapshotFile/).

## NAS primary-fileset assignments

`Client.NASFilesetSources` / `FetchNASFilesetSources` return paginated NAS
shares with share and primary-fileset IDs, protocol, host, export path, cluster,
and primary-fileset configured, effective and pending SLA references.
Missing response fields, failed pages and nonadvancing/cyclic cursors return
errors without partial results. The existing SLA retry policy applies.

`Client.Assignments` / `FetchSLAAssignments` use primary-fileset configured or
effective policy matches rather than parent-share or reporting associations.
Explicit `DO_NOT_PROTECT`/`UNPROTECTED` and unknown effective state are preserved;
configured membership does not imply protection. IDs take precedence over names.
Pending SLA is exposed as metadata but does not alone establish membership or
effective protection. NAS assignments use kind `NAS_FILESET` and normalize SMB
to CIFS. Other assignment kinds remain unchanged.

For exports across multiple policies, fetch the inventory once and pass it to
`Client.AssignmentsWithNAS` / `FetchSLAAssignmentsWithNAS`. A non-nil empty slice
means authoritative empty inventory; nil requests a fresh inventory. The caller
owns its scope and lifetime; the library adds no cache.

## Backup jobs

`Client.BackupJobs(ctx, BackupJobsOptions)` / `FetchBackupJobs` list backup
activity series, including status, progress, workload UUID (`WorkloadID`),
cluster UUID, activity-series UUID, and nullable `IsCancelable`/`IsOnDemand`.
These IDs are distinct from the CDM `ObjectID`. The read always filters for
backup activities; optional filters include workload, cluster, search term, and
the tenant's `EventStatus` values. Historical and running jobs are included
unless a status filter is supplied. Existing `BackupActivities` is unchanged.

Each call fetches one page: `First` is 1-100 (default 100), `After` is the
previous page's opaque `EndCursor`, and `HasNextPage` indicates continuation.
Callers own traversal limits, cursor-cycle detection across calls, and deadlines.
Missing connections, identities, page status, and nonadvancing continuation
cursors return errors without partial pages. Successful empty pages have
non-nil `Jobs` slices.

`Client.StartBackup(ctx, StartBackupInput)` / `StartBackup` submit on-demand
backups through `takeOnDemandSnapshot`. Supply nonempty, unique workload UUIDs
in `WorkloadIDs` and a retention `SLAID`, which the API permits to be empty.
Only workloads supported by the tenant's operation can be started.

```go
result, err := client.StartBackup(ctx, rsc.StartBackupInput{
    WorkloadIDs: []string{workloadID},
    SLAID:       retentionSLAID,
})
```

Always inspect `result.Taskchains` and `result.Errors`, including when `err`
is non-nil: a batch can partially succeed. Missing or inconsistent outcomes
also return an error and preserve the returned result. Taskchain IDs acknowledge
submission, not successful backup completion. A transport or protocol failure
may leave the submission outcome unknown; reconcile before retrying, rather than
blindly repeating the batch.

`Client.CancelBackup(ctx, CancelBackupInput)` / `CancelBackup` cancel a listed
activity using both its `ActivitySeriesID` and `ClusterID`.
`Client.CancelBackupTaskchain(ctx, taskchainID)` / `CancelBackupTaskchain` use
the taskchain ID returned by initiation instead. Do not interchange these IDs.
`IsCancelable == nil` means unknown; permissions and current cancelability remain
server-authoritative. Cancel calls return `true` only when RSC acknowledges the
request; rejection or a missing acknowledgement returns an error. Acknowledgement
does not prove that the job has reached its terminal canceled state; check the
job status afterward.

All four new operations use the caller's authorized HTTP client through the SDK,
with one attempt per call and no automatic mutation replay, fallback, polling,
or added credentials. Use contexts with deadlines. Operation availability,
workload coverage, permissions, and actual execution require tenant validation;
tests use synthetic fixtures and never start or cancel live backups.

Schema contracts: [takeOnDemandSnapshot](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/mutations/takeOnDemandSnapshot/),
[cancelActivitySeries](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/mutations/cancelActivitySeries/),
and [cancelTaskchain](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/mutations/cancelTaskchain/).

## Backup activity monitoring

`Client.ReadBackupActivityObservations` / `ReadBackupActivityObservations` add
bounded, presence-aware monitoring reads without changing `BackupActivities`,
`BackupJobs`, snapshot enrichment, or their legacy behavior. This API returns
**activity-series observations, not verified backup executions**.
`BackupActivitySourceCapabilities()` explicitly reports the unsupported
execution identity, whole-run status, completion, cumulative-counter, duration,
and atomic-enumeration capabilities. No live probes were used.

```go
end := time.Now().UTC()
read, err := client.ReadBackupActivityObservations(ctx, rsc.BackupActivityReadOptions{
        Scope: rsc.BackupActivityScope{
                WorkloadIDs: []string{workloadID},
                ClusterIDs:  []string{clusterID},
        },
        Window: rsc.BackupActivityWindow{
                Start: end.Add(-time.Hour),
                End:   end,
                Field: rsc.BackupActivityUpdatedTime,
        },
        Statuses: []string{"RUNNING", "QUEUED", "CANCELING"},
        Limits: rsc.BackupActivityReadLimits{
                PageSize: 100, MaxPages: 10, MaxResults: 1000,
                MaxDuration: 30 * time.Second,
        },
})
```

Scope requires 1-100 exact workload UUIDs and/or 1-100 exact cluster UUIDs; when
both are supplied they intersect. Returned identities are validated against
that scope and canonicalized to lowercase. `WorkloadID()` is the source `fid`,
suitable for joining the immutable IDs in bounded SLA/source inventories.
It is not the display name or the CDM `objectId`. `ActivitySeriesID()` is only
a series UUID; it is not relabeled as an execution, snapshot, or child-task ID.

Windows require explicit increasing UTC bounds and select either
`BackupActivityUpdatedTime` (`lastUpdatedTimeGt`/`lastUpdatedTimeLt`) or
`BackupActivityStartTime` (`startTimeGt`/`startTimeLt`). Source bounds are
**exclusive at both ends**, not a half-open interval. The selected timestamp
must be known and strictly inside the window; other timestamps may be null.
Omit `Statuses` for update/history reads that also retain unknown statuses.
Filters use exact `EventStatus` values: `SUCCESS`, `FAILURE`, `CANCELED`, and
`PARTIAL_SUCCESS` select latest-activity outcomes, not authoritative terminal
backup runs. `TASK_SUCCESS`/`TASK_FAILURE` are not normalized into run success
or failure. Returned `LastActivityStatus()` always preserves the raw value.

All limits are explicit: 1-100 entries/page, 1-100 total pages, 1-10000 total
results, and an overall duration greater than zero and at most one minute.
Each page has a 4 MiB response cap and 4096-byte cursor cap and uses one SDK
attempt through the authorized HTTP client. Shorter caller deadlines win.
Missing selected fields, malformed values, invalid/out-of-scope identities,
duplicate series, cyclic/stalled cursors, page failures, and exceeded limits
fail closed. A duplicate, even an identical one, is a failed read rather than
silently deduplicated mutable pagination.

Only `err == nil && read.Complete` supplies observations; successful empty
slices are non-nil. Failed reads return nil observations and `Complete == false`
with window/scope, collected-page/result counts, and collection times for
diagnostics. `Continuation` is diagnostic only, not a safe resume token or a
consumer checkpoint. Never advance a checkpoint on failure. `Atomic` is always
false: draining a mutable connection proves neither an atomic point-in-time
view nor that changes during the traversal were all seen. Narrow/repeat windows
and allow overlap; a drained start-time window is not historical event retention.

Observations have private value fields and value-returning getters. Null numeric
and time values return `known == false`; genuine zero remains known. Previously
returned observations cannot be changed by later reads or mutated option slices.
Extract getters into consumer-owned persistence records, retaining their known
flags; default JSON marshaling of an observation produces `{}`, not source data.
`RevisionFingerprint()` hashes the selected canonical source values, including
nullable presence and retry metadata, but excludes local `CollectedAt()`. It is
a content revision for replay, not an execution ID, ordered source version, or
guarantee that the source retained every intermediate update.

### Verified source meanings

The public schema and local SDK fixtures were checked on 2026-11-03:

| Source field | Meaning and limit |
| --- | --- |
| `startTime` | Series start, nullable; not a transfer-phase boundary. |
| `lastUpdated` | Series update time; **not completion**. |
| `lastEventAddedAt` | Most recent event-added time; not completion. |
| `lastVerifiedAt` | Most recent verification time; not completion. |
| `CollectedAt()` | Local UTC collection receipt, not source freshness. |
| `dataTransferred` | Nullable event-associated bytes. Cumulative vs incremental vs final-only, compression, and physical/logical semantics are unspecified. |
| `logicalSize` | Separate nullable logical-size bytes, not transferred bytes. |
| `attemptNumber` | Nullable related-job attempt metadata; retry grouping and series reuse are unspecified. |

No completion or duration is derived from series updates, counters, status,
message text, names, or an absent row. Counter decreases, retry changes, late
updates, and cancellation remain observable source revisions; they are not
interpreted as a counter reset, a new execution, an abort, or a completion.
The legacy enrichment duration `LastUpdated - StartTime` is not a verified
monitoring duration. There are no verified transfer-phase boundaries or duration
in these selections, so transfer duration/rate and physical-byte deltas are not
exposed. Without a verified execution identity and cumulative-counter contract,
do not turn `dataTransferred` observations into traffic totals or rates.

### Known job-instance taskchains

`Client.ReadKnownBackupTaskchains` / `ReadKnownBackupTaskchains` read explicit
caller-known taskchain UUIDs. `TaskchainIDs`, `MaxRequests` (1-100), and
`MaxDuration` (greater than zero, at most one minute) are required; all requested
IDs must fit the request budget. The same SDK path and 4 MiB response cap apply,
with one attempt per ID and no partial batches. `Complete` concerns that supplied
ID set only, and `Atomic` remains false. This is lookup, not execution discovery.

```go
taskchains, err := client.ReadKnownBackupTaskchains(ctx, rsc.BackupTaskchainReadOptions{
        TaskchainIDs: []string{taskchainID},
        MaxRequests:  1,
        MaxDuration:  10 * time.Second,
})
```

The `taskchain` schema provides job-instance UUID, state, nullable start/end,
last progress time, parent ID, raw job type, and current **task** execution
attempts. `Terminal()` recognizes `SUCCEEDED`, `FAILED`, and `CANCELED`; `READY`
(waiting), `RUNNING`, `CANCELING`, and `UNDOING` are nonterminal. `UNKNOWN` and
future values remain raw and return unknown terminal classification. Cancellation
is not renamed "aborted" or assumed instantaneous. `ElapsedDuration()` returns
end minus start only for a known terminal state with known ordered boundaries
and representable duration, including a genuine zero. It is taskchain elapsed
time, not transfer-phase time, queued time, or necessarily a backup's duration.

The initiation reply documents a workload UUID/job-instance taskchain UUID pair;
retain that source binding from `StartBackupResult`. Do not invent bindings for
scheduled backups. The lookup does not establish workload/cluster identity,
backup workflow classification, or parent/root semantics. A child taskchain's
state is authoritative only for that child. The public activity selections do
not expose a verified link to job-instance taskchains or enumerate all scheduled
executions. Task-internal attempts do not create a new instance while the source
UUID stays the same; whether a whole-backup retry reuses or replaces that UUID
remains unverified. No fallback to `getKorgTaskchainStatus`, CDM, or parsed opaque
activity metadata is attempted.

### Consumer integration contract

- Reconcile active **candidates** using exact scope and bounded windows. Old/stale
    runs may fall outside a window, and mutable pagination may omit a revision.
    Missing candidates never imply completion or abort. Retain independently
    known instance IDs and reconcile them through their authoritative source.
- Record collection receipt separately from source update/verification times.
    A fresh poll is not a fresh source observation; null means unknown.
- Keep overlapping update windows and deduplicate series revisions by source
    series UUID plus fingerprint. Handle late corrections without treating them
    as new executions. Source update time is not a completion timestamp or proof
    of a monotonically increasing revision sequence.
- For independently bound job-instance taskchains, store terminal history by
    instance UUID, updating revisions/duration rather than counting each terminal
    poll. Do not aggregate unrelated series/children into a run or deduplicate
    concurrent runs by workload UUID. Missing/denied lookups are not terminal.
- Cumulative-counter deltas require verified counter mode, the same execution
    and attempt, ordered source observations, and non-null nondecreasing values.
    A decrease or attempt change invalidates the baseline; never take absolute
    differences or treat null as zero. This source's counter mode is unverified,
    so retain raw values without emitting cumulative deltas yet.

Polling, persistence, deduplication checkpoints, Influx encoding, and dashboards
remain consumer-owned. `BackupMonitoringError` exposes sanitized error text and
typed invalid-option/incomplete/limit/request/HTTP-denied codes while preserving
`errors.Is` cancellation, deadlines, and authority identity. Do not log unwrapped
SDK errors, which may include source content. HTTP 401/403 are classified as
denied; a GraphQL error in HTTP 200 remains a generic request failure unless a
documented structured denial contract is available, never classified from text.
Full scheduled-execution monitoring and verified byte-rate/transfer-duration
metrics remain blocked by source semantics, not filled with guessed values.

References: [ActivitySeries](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/objects/ActivitySeries/),
[ActivitySeriesFilter](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/inputs/ActivitySeriesFilter/),
[ActivityStatusEnum](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/enums/ActivityStatusEnum/),
[EventStatus](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/enums/EventStatus/),
[Taskchain](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/objects/Taskchain/),
[TaskchainState](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/enums/TaskchainState/),
and [TakeOnDemandSnapshotTaskchainUuid](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/objects/TakeOnDemandSnapshotTaskchainUuid/).

## Cluster member load

`Client.NodeStats(ctx, clusterFilter, options)` and
`FetchNodeStatsForCluster` retrieve RSC's `Cluster.clusterNodeStats` without
changing existing health or configuration reads. Results contain cluster identity
and timestamped samples keyed by node ID. Multiple samples per node can be
returned; these are RSC observations, not a direct, live poll of each member.

```go
end := time.Now().UTC()
stats, err := client.NodeStats(ctx, "lab*", rsc.NodeStatsOptions{
    Start:       end.Add(-15 * time.Minute),
    End:         end,
    Aggregation: "AVERAGE",
})
```

Options accept a paired start/end range, `AVERAGE`, `MAX`, or `UNDEFINED`
aggregation, and an optional `NodeID`. Zero options leave the range, aggregation,
and node selection to the API defaults. Cluster results are paginated.
`MaxPages` accepts 1-10 (default 10), and `MaxSamples` accepts 1-10000
(default 10000), globally across cluster pages. Reads have a maximum one-minute
deadline, one SDK attempt per page, 100 clusters per page, 4 MiB per response,
and 4096 bytes per cursor. Incomplete envelopes or exceeded bounds return no
partial result. Caller deadlines remain authoritative when shorter.

Samples expose `CPUStat` (raw API CPU utilization; the public reference does not
specify its scale), `UsedMemoryPercent` (RAM used, percent), read/write IOPS
(operations/second), and read/write throughput (bytes/second). Metric pointers
distinguish missing/null values from zero. Set `IncludeExtended: true` to also
request disk utilization in basis points (divide by 100 for percent) and the
five-minute load average in thousandths (divide by 1000). Those newer fields
are opt-in to avoid requiring them on older tenant schemas. A tenant lacking
the operation or a requested field returns a GraphQL error, not fabricated zeros
or a silent fallback. No direct CDM credentials or transport are added.

Schema contracts: [Cluster](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/objects/Cluster/)
and [ClusterNodeStats](https://developer.rubrik.com/Rubrik-Security-Cloud-API/API-Reference/types/objects/ClusterNodeStats/).

## Local development and SDK fork

`FilesetInventory(ctx, InventoryLimits{MaxPages: 30, MaxObjects: 10000})` is an
additive monitoring read; legacy fileset methods retain their original behavior.
It requires a deadline and traverses the Windows/Linux/NAS fileset roots through
the SDK with one attempt per page. Limits are global across roots: 3-30 pages,
1-10000 objects, 100 templates/descendants per connection, 4 MiB per response and
4096 bytes per cursor. It fails closed on missing connections, nodes/page status,
nonadvancing/cyclic cursors, duplicates, missing immutable workload/cluster IDs,
truncated descendants or any page failure. Errors return no partial inventory.
Successful empty slices are non-nil; incomplete results wrap/return an error.
`EffectiveSLAID` preserves presence: nil is unknown, not unprotected. The caller
must verify tenant permissions, source scope and policy semantics before using
these fields; this API does not certify SLA compliance or live tenant support.
It creates no cache, persistence, daemon or parallel authentication layer.

This module is published at https://github.com/otuschhoff/rsc-go. It includes a narrowly patched SDK in
[third_party/rubrik-polaris-sdk-for-go](third_party/rubrik-polaris-sdk-for-go).
See [SDK_FORK.md](SDK_FORK.md) for the base revision, exact extension and upgrade
procedure. Go does not propagate dependency `replace` directives. Consumers must
explicitly replace the SDK with the nested module at the recorded SDK revision:

```go
require github.com/otuschhoff/rsc-go v0.3.0

replace github.com/rubrikinc/rubrik-polaris-sdk-for-go => github.com/otuschhoff/rsc-go/third_party/rubrik-polaris-sdk-for-go v0.0.0-20261102173521-d9afd9c9fb40
```

For local sibling development, replace rsc-go with `../rsc-go` and change the
SDK replacement to `../rsc-go/third_party/rubrik-polaris-sdk-for-go`. No local
replacement is needed for normal consumers.

Run independently:

```sh
go test -race ./... -count=1 -timeout=3m
go vet ./...
cd third_party/rubrik-polaris-sdk-for-go
go test -race ./pkg/polaris/graphql ./pkg/polaris/token -count=1 -timeout=3m
```

The root test command does not traverse nested modules. Tests use synthetic
credentials and local/fake transports, not live RSC. A runnable monitoring daemon
and upstream contribution remain separate follow-up
work; no daemon is installed by this library.
