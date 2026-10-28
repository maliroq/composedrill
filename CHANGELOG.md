# Changelog

## [Unreleased]

- Final presentation/distribution pass: added the ComposeDrill project mark, GitHub social card, terminal hero asset, concise README hero/flow, installation docs, contribution fixture hook, and four-target release packaging metadata.
- Rehearsal terminal output now leads with a fixed-width recovery summary (`BACKUP / MANIFEST`, `DATA RESTORE`, `CONTAINER STARTUP`, `APPLICATION HEALTH`, `RESULT`), uses ANSI status color only on TTYs, honors `NO_COLOR`, and stays plain in redirected output. Exit codes and recovery semantics are unchanged.
- Adversarial RC audit: cleanup failures and HIGH/CRITICAL runner findings now force overall FAIL; evidence-write errors are surfaced instead of ignored.
- Directory-artifact hashes now use a versioned length-framed encoding; bind fixture manifests were regenerated for the new hash contract.
- Explicit checks files and interpolation `.env` files must be declared/checksummed inside the recovery bundle before use.
- Disabled Docker healthchecks no longer count as verification, and explicit checks are applied to every running replica of scaled services.
- Expanded unsafe-runtime refusal to namespace-sharing, `volumes_from`, capability additions, external links/`extra_hosts` entries, and unconfined security options.
- Named-volume tar archives are validated before extraction and restored through the pinned, network-disabled utility image; the release gate now includes named and anonymous volume restore tests plus a MariaDB logical-restore row assertion.
- Image readiness now treats only digest references as immutable pins; mutable version tags remain unpinned.
- Run IDs now use a 48-bit `crypto/rand` suffix instead of a timestamp-derived short suffix.
- Strengthened evidence redaction for embedded sensitive assignments and URL credentials.

- Reworked HTTP/TCP checks to avoid host port publication entirely: recovery networks remain `internal: true`, production port mappings are removed, and checks run from a short-lived capability-dropped probe container on the exact labeled recovery network using a pinned multi-platform BusyBox image digest.
- Added fail-closed probe-network verification for project namespace, run/managed labels, `Internal=true`, malformed container/network inspection, and semantic HTTP status matching.
- Improved Docker command failures so an empty stderr still records the actual execution error, and preserved a clear bounded timeout when the port-assignment context expires.
- Hardened `dev.py` integration-result parsing so redacted PASS-line names do not make successfully executed tests appear missing; exit-code, FAIL, and SKIP gates remain strict.
- Added deterministic macOS `python3 dev.py` release-gate harness with Docker prerequisite detection, evidence-aware broken-fixture validation, cleanup audit, fixture immutability checks, redacted logs, and JSON/text reports.

### Added

- Compose discovery and recovery-readiness scan
- human recovery plan and portable manifest
- checksummed recovery-bundle format
- isolated Compose rewrite with internal networks and no recovery-service host port publications
- bind/named-volume restore paths
- PostgreSQL and MySQL/MariaDB logical restore support
- Docker healthchecks and user-defined HTTP/TCP/command/DNS/file checks
- label-scoped cleanup
- stateless, PostgreSQL, SQLite, bind, and intentionally broken fixtures
- unit, failure-injection, interruption, redaction, malformed-Compose, archive-safety, and Docker integration tests

### Hardened

- command checks can require an expected output marker instead of trusting exit code alone
- service env files and file-backed secrets/configs must remain inside the checksummed bundle
- directory artifacts reject symlinks and special files during checksum verification
- named-volume helper containers carry full cleanup ownership labels
- profile-gated services stay manual rather than being activated implicitly
- cleanup removes only the selected run workspace after label-scoped Docker cleanup
- all five synthetic fixtures have Docker integration-test coverage
