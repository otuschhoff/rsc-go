# rsc-go

Reusable Rubrik Security Cloud inventory, monitoring, SLA, NAS/fileset and
snapshot routines, extracted from opsctl. Module:
`github.com/otuschhoff/rsc-go`; package name: `rsc`.

Current release: `v0.1.1`. See [CHANGELOG.md](CHANGELOG.md) for release notes.

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
workload snapshots, backup activities and snapshot/activity enrichment.
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
require github.com/otuschhoff/rsc-go v0.1.1

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
