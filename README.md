# rsc-go

Reusable Rubrik Security Cloud inventory, monitoring, SLA, NAS/fileset and
snapshot routines, extracted from opsctl. Module:
`github.com/otuschhoff/rsc-go`; package name: `rsc`.

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

## Local development and SDK fork

This module is published at https://github.com/otuschhoff/rsc-go. It includes a narrowly patched SDK in
[third_party/rubrik-polaris-sdk-for-go](third_party/rubrik-polaris-sdk-for-go).
See [SDK_FORK.md](SDK_FORK.md) for the base revision, exact extension and upgrade
procedure. Go does not propagate dependency `replace` directives. Consumers must
explicitly replace the SDK with the nested module published at the same commit:

```go
require github.com/otuschhoff/rsc-go v0.0.0-20261102173521-d9afd9c9fb40

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
credentials and local/fake transports, not live RSC. A runnable monitoring daemon,
tagged release and upstream contribution remain separate follow-up
work; no daemon is installed by this library.
