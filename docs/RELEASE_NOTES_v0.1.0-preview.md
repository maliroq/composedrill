# ComposeDrill v0.1.0-preview

**Your backup passed. Will your app?**

ComposeDrill is a Docker Compose recovery-rehearsal CLI. It validates a checksummed recovery bundle, restores persistent state into disposable ComposeDrill-owned resources, starts the recovered stack, runs application checks, writes evidence, and reports PASS, FAIL, or MANUAL where automatic proof is not justified.

It is intentionally not a backup scheduler or storage backend.

## What this preview proves

The release gate requires:

- unit tests;
- race tests;
- `go vet`;
- production build;
- Docker integration tests with zero skips;
- stateless, PostgreSQL, SQLite, and bind recovery rehearsals;
- named-volume and anonymous-volume restore coverage;
- MariaDB logical-restore coverage;
- an intentionally broken backup that must reach startup successfully and then fail application health;
- zero stray ComposeDrill containers, networks, or volumes after cleanup;
- no mutation of the fixture tree used as production state.

The final audited source candidate passed that complete gate on macOS arm64 with Docker Desktop.

## Recovery behavior

A normal drill separates:

```text
BACKUP / MANIFEST        PASS
DATA RESTORE             PASS
CONTAINER STARTUP        PASS
APPLICATION HEALTH       FAIL

RESULT                   FAIL
```

That FAIL is useful: the backup existed and restored, but the application still did not recover.

## Safety boundaries

Recovery services use a random Compose project, disposable bind/volume state, internal networks, and no host port publications. HTTP/TCP checks run from a constrained tool-owned probe on the verified internal recovery network. Unsafe runtime/namespace-sharing modes and undeclared host-file inputs are refused rather than normalized away. Cleanup requires matching run/managed ownership labels and namespace checks.

## Packaging

Release archives are built for:

- macOS arm64;
- macOS amd64;
- Linux amd64;
- Linux arm64.

Go is not required to run a release binary. Published archives are accompanied by SHA-256 checksums. Windows support is not claimed in this preview.

The macOS arm64 target has the full real-Docker release-gate evidence. Linux amd64 is exercised by the GitHub release workflow's Docker integration job before publication. The other archive targets are cross-built and are not presented as equivalent platform-level Docker evidence.

## Deliberate limitations

The project does not hide these boundaries:

- Docker Compose only;
- profiles remain manual;
- Docker is not treated as a hardened sandbox;
- archive validation has no global expanded-size quota yet;
- directory-form bundles have a documented TOCTOU boundary;
- manifests/evidence are checksummed, not cryptographically signed;
- PostgreSQL custom-format and MySQL logical restore paths are implemented but not independently real-Docker gated in v0.1;
- DNS checks and scaled-service aggregation do not yet have dedicated real-Docker fixtures;
- several synthetic fixture images use mutable version tags and are flagged as such.

See `docs/KNOWN_LIMITATIONS.md` and `docs/SUPPORT_MATRIX.md` for the complete evidence boundary.
