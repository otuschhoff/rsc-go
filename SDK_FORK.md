# Minimal SDK transport extension

Base: `github.com/rubrikinc/rubrik-polaris-sdk-for-go v1.11.0`, upstream commit
`2d34d7f2ec1745c7ee4e06a057eb31b4e9ec3744`.

The [vendored source](third_party/rubrik-polaris-sdk-for-go) retains the upstream
module identity, original files and [MIT license](third_party/rubrik-polaris-sdk-for-go/LICENSE).
Its source is published inside the rsc-go repository, including as a nested Go
module. It is not an upstream release or a separately tagged fork.

## Delta

Only two files are added under `pkg/polaris/graphql`:

- `http_client.go`: `NewClientWithHTTPClient(endpoint, httpClient)` constructs the
  existing GraphQL client using a complete URL and application-owned HTTP client.
- `http_client_test.go`: validation, exact URL/payload, injected transport,
  cancellation and no eager network/authentication tests.

No upstream authentication, retry, domain-feature, query or module files change.
No reflection, unsafe, global transport replacement or test-only constructors
are used. In particular, authorization is not copied out of opsctl's authority.

The extension deliberately does not install a token source, add Authorization,
append `/graphql`, read credentials/environment settings, or enable token caching.
The caller must supply appropriate authorization and redirect policy. rsc-go
wraps a copy of the HTTP client per operation, blocks redirects, preserves status
and underlying transport errors, and delegates execution to
`RequestWithoutRetry`. Its bounded retry wrapper lives in rsc-go, not this fork.

## Upgrade and upstream plan

1. Compare a new published SDK tag with the recorded base and rerun opsctl's
   independent `tools/rscsdkassessment` module against the **unmodified** release.
2. Replace the upstream tree without modifying or dropping its license/headers.
   Reapply only the constructor and its focused tests if still needed.
3. Rerun rsc-go's complete race tests/vet, fork GraphQL/token tests and opsctl's
   RSC, credential and directly related consumer regressions.
4. Reassess prepared-operation field/pagination parity before replacing custom
   queries. Do not lose data just to use typed SDK methods.
5. Propose the authenticated-client constructor upstream when publication is
   authorized. If an equivalent supported upstream hook lands, remove the local
   extension and both explicit SDK replacements only after the same tests pass.

Go `replace` directives are not transitive. Every consumer needs its own SDK
replacement, as shown in [README.md](README.md). Both rsc-go and its nested SDK
module can be pinned to the same published commit through a Go pseudo-version;
no sibling checkout is required. Publication does not remove the need for the
explicit replacement. An accepted upstream extension would allow its removal.
