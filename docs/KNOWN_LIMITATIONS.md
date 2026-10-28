# Known limitations — v0.1.0-preview

- Docker Compose only. Swarm, Kubernetes, VM, and orchestration-specific recovery are out of scope.
- The static scanner intentionally implements a lightweight Compose subset. Rehearsal relies on Docker Compose's canonical model and refuses source features that can read undeclared host files (`include`, `extends`, `label_file`).
- Compose profiles are not activated automatically. Profile-gated services remain manual.
- One-shot/init containers are not specially modeled.
- Recovery networks are `internal: true` and recovery services publish no host ports, but Docker is not treated as a security sandbox. Run untrusted images on a dedicated host/VM.
- External SaaS and outbound dependencies are not emulated. A service that needs them requires a safe mock or manual verification.
- Archive validation rejects traversal, links, and special files, but v0.1 does not impose a global archive entry-count or expanded-byte quota. Treat backup bundles as operator-controlled input and use host-level resource limits for hostile archives.
- Generic tar restore does not promise preservation of every filesystem feature such as ACLs, xattrs, ownership semantics across platforms, sparse-file layout, or device nodes (device nodes are refused).
- Directory-form bundles are checksum-validated and then consumed in place. Concurrent mutation after validation is outside the integrity guarantee; use an immutable snapshot or tar-staged bundle to close that race.
- Bundle manifests and evidence are not cryptographically signed. Checksums detect byte mismatch but do not authenticate a malicious party capable of rewriting both artifact and manifest.
- Evidence redaction covers common sensitive assignments, JSON-style sensitive keys, and URL credentials. It is defense-in-depth, not a general DLP system; custom application output can still contain secrets in unexpected forms.
- PostgreSQL custom-format restore and MySQL logical restore are implemented but do not have independent v0.1 real-Docker release fixtures. MariaDB and PostgreSQL plain SQL do.
- DNS checks depend on `getent` being present in the target image and have no dedicated v0.1 Docker release fixture.
- Scaled-service aggregation has unit regression coverage but no dedicated scaled-service Docker fixture in v0.1.
- SQLite recovery can prove that the supplied restored file works for the fixture/application check; ComposeDrill cannot infer whether an arbitrary SQLite file was captured transaction-consistently. Capture it with SQLite-safe backup semantics.
- Rehearsal writes `.composedrill/` workspace/evidence beneath the supplied root. It does not promise the root directory is byte-for-byte untouched.

- Several synthetic fixture service images use version tags rather than immutable digests. The scanner correctly flags those references as mutable; the release gate proves behavior against the images resolved for that run, not a cryptographically fixed upstream image set. The ComposeDrill utility/probe image itself is digest-pinned.

- Custom volume/network drivers and driver options, service `security_opt`, GPU/custom-runtime/cgroup escape surfaces, and unknown mount types are refused rather than emulated. Recovery service logging is disabled to prevent daemon-side external logging drivers from bypassing internal-network isolation.

- The full v0.1.0-preview real-Docker release gate has been run on macOS arm64 with Docker Desktop. Linux amd64 is exercised by the GitHub release workflow before publication; macOS amd64 and Linux arm64 are cross-built archive targets without equivalent platform-specific Docker gate evidence in this preview.
