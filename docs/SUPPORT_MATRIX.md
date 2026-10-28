# v0.1 support matrix

This matrix distinguishes implementation from release-gate evidence. A capability is not called Docker-verified merely because a unit test exists.

| Area | v0.1 status | Evidence / boundary |
| --- | --- | --- |
| Compose discovery | Supported | Static scanner recognizes the four standard Compose filenames and inventories services, mounts, ports, env-file references, secrets, dependencies, profiles, active healthchecks, digest pinning, and selected unsafe runtime modes. Advanced YAML can exceed the lightweight scanner. |
| Compose normalization for rehearsal | Supported | Uses `docker compose config`; `include`, `extends`, and `label_file` are refused before canonicalization in v0.1. |
| Isolation project names | Supported | Cryptographically random 48-bit suffix plus timestamp; recovery resources are namespaced and labeled. |
| Production bind mounts | Supported isolation | Rewritten to run-local paths; production bind sources are not mounted into recovery services. |
| Named volume restore | Supported | Tar archive restore into a disposable labeled volume; dedicated real-Docker integration test is part of the release gate. |
| Anonymous volume restore | Supported | Artifact must be mapped by service + target; dedicated real-Docker integration test is part of the release gate. |
| Bind artifact restore | Supported | Directory, tar, tar.gz/tgz. Traversal, links/symlinks, and special files are refused. |
| PostgreSQL plain SQL | Supported | `psql -v ON_ERROR_STOP=1`; real-Docker fixture asserts restored row content. |
| PostgreSQL custom format | Implemented, not independently Docker-gated | `pg_restore --exit-on-error --clean --if-exists`; v0.1 release gate does not include a custom-format fixture. |
| MariaDB logical SQL | Supported | Real-Docker integration fixture is part of the release gate and asserts restored row content. |
| MySQL logical SQL | Implemented, not independently Docker-gated | Shares the `mysql-sql` path, but v0.1 does not claim separate MySQL Docker evidence. |
| SQLite | Supported with capture caveat | Restore path is Docker-gated. ComposeDrill does not prove that an input SQLite file was captured consistently; use SQLite online-backup/safe snapshot semantics. |
| Docker healthchecks | Supported | Active healthchecks are evaluated separately from container startup. Disabled/NONE healthchecks do not count. |
| HTTP checks | Supported | Run from a constrained tool-owned container on the verified internal recovery network; release fixture exercises it. |
| TCP checks | Supported | Same internal probe mechanism; stateless public fixture includes a TCP check in the audited release candidate. |
| Command checks | Supported | Run inside every running replica; optional `expect_contains` adds semantic assertion. PostgreSQL/MariaDB fixtures exercise command assertions. |
| File-exists checks | Supported | Run inside every running replica; SQLite fixture exercises it. |
| DNS checks | Supported with image dependency | Uses `getent hosts` inside each target container. Images without `getent` fail the check; there is no dedicated v0.1 Docker fixture for DNS. |
| Scaled services | Supported for startup/health/check aggregation | All observed replicas must be running; explicit checks run against every running replica. Unit-regression covered; no dedicated scaled Docker release fixture. |
| Compose profiles | Manual | Profiles are not activated automatically; profile-gated services remain `MANUAL CHECK REQUIRED`. |
| One-shot/init containers | Not specially modeled | May require a manual recovery check. |
| External secrets/configs | Unsupported automatically | Refused. File-backed env/secrets/configs used by rehearsal must stay in the checksummed bundle. |
| Custom volume/network drivers | Unsupported automatically | Host-capable driver settings/options are refused; v0.1 uses ordinary disposable local volumes and internal bridge networks. |
| Daemon-side logging drivers | Neutralized | Recovery services use Docker logging driver `none` so logging cannot bypass internal networking via the daemon. |
| Outbound SaaS dependencies | Not emulated | Internal recovery networks block normal routed external egress. Safe mocks or manual verification are required. |
| Restic/Kopia/Backrest/Duplicati direct adapters | Not in v0.1 | Stage/export data into the recovery-bundle contract. |
| Swarm/Kubernetes/VMs | Unsupported | Compose-only v0.1. |

## Release platform evidence

| Platform | Archive | v0.1 evidence boundary |
| --- | --- | --- |
| macOS arm64 | Yes | Full real-Docker release gate on Docker Desktop |
| macOS amd64 | Yes | Cross-built; no independent platform Docker gate |
| Linux amd64 | Yes | GitHub release workflow runs the Docker integration suite before publication |
| Linux arm64 | Yes | Cross-built; no independent platform Docker gate |
| Windows | No | Not claimed supported in v0.1.0-preview |
