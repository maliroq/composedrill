# Safety model

ComposeDrill treats recovery rehearsal as a potentially destructive operation and designs around the assumption that production must survive a bug, interruption, or bad backup.

## Invariants

1. `scan`, `plan`, and `manifest` never invoke Docker mutations.
2. Rehearsal never intentionally targets the production Compose project name.
3. Every recovery project name begins with `composedrill-` and includes a random run suffix.
4. Every managed service, volume, and network is labeled with both `io.composedrill.run=<run-id>` and `io.composedrill.managed=true`.
5. Bind mounts are rewritten to run-local paths. The production bind source is never mounted into the recovery service.
6. Named volume names are rewritten. External or explicitly named production volumes are never used as restore targets.
7. Recovery networks are `internal: true`, blocking normal routed external egress through the recovery network by default; this is not a claim that Docker is a hardened sandbox.
8. Host port publications are removed from recovery services. HTTP/TCP checks run from a short-lived tool-owned probe container that is allowed to join only the exact labeled `internal: true` recovery network.
9. Cleanup verifies exact run labels and namespace before deleting anything.
10. Cleanup never executes `docker compose down -v` against a production project.
11. Bundle/bind extraction and named-volume archive validation refuse absolute paths, `..` traversal, symlinks, hard links, devices, and other special files.
12. Privileged/device/GPU access, custom runtimes, namespace/cgroup sharing, `volumes_from`, capability additions, external links, `extra_hosts`, and service `security_opt` are conservatively refused for automatic rehearsal. Non-local/custom volume drivers and non-bridge/custom network driver options are refused, and daemon-side service logging is neutralized to `none`.
13. External Docker secrets/configs and secret/config files outside the recovery bundle are refused. Service `env_file`, interpolation `.env`, and selected checks files are constrained to declared, checksummed files inside the bundle before use.
14. Compose `include`, `extends`, and `label_file` are refused in v0.1 so canonicalization cannot silently ingest undeclared host files.
15. Missing mappings are `MANUAL CHECK REQUIRED`; they are never silently treated as empty-but-valid state.
16. Profile-gated services are not activated implicitly; they remain manual.
17. Container `running` is not recovery proof. Active health or an explicit check must pass; disabled healthchecks do not count.
18. HIGH/CRITICAL runner findings, cleanup failures, and evidence-write failures cannot be downgraded to PASS.

## Interruption behavior

SIGINT and SIGTERM cancel active Docker commands and the runner proceeds to label-scoped cleanup using a fresh short-lived cleanup context. SIGKILL, kernel failure, daemon crash, or host power loss cannot be handled synchronously; orphaned resources retain their run labels and can be removed with:

```bash
composedrill cleanup --run <run-id> --root <original-scan-root>
```

The cleanup command refuses an empty/invalid run identity, removes only resources that match the expected labels and ComposeDrill namespace, and then removes only that run's workspace beneath `<root>/.composedrill/runs/`. Evidence is preserved separately.

## Secrets

Normal scan output reports environment variable names, secret object names, and missing file names—not values. Rehearsal normalizes Compose with `docker compose config --no-interpolate` and a scrubbed process environment. Evidence is JSON written mode `0600`; common sensitive assignments and URL credentials are redacted before persistence. Redaction is defense-in-depth, not a substitute for protecting the backup/rehearsal host.

A recovery bundle may itself contain secrets because a real recovery often needs them. Protect the bundle as backup material. ComposeDrill does not weaken that requirement.

## Local state

Rehearsal writes `.composedrill/` beneath the root passed to the command for temporary run state and persistent evidence. Production application bind sources are not used as recovery targets, but the root directory itself is therefore not byte-for-byte read-only during rehearsal.

## `--keep`

`--keep` exists for debugging. It intentionally leaves the disposable containers/volumes/networks and the run workspace present. The workspace may contain restored application data and secret-bearing backup material. Do not use `--keep` casually on shared hosts. Use the run ID with `cleanup` when finished.
