# Changelog

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