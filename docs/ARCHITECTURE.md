# Architecture

ComposeDrill is intentionally a CLI-first Go project with no daemon or dashboard in v0.1.

## Packages

- `internal/compose`: Compose-file discovery and lightweight read-only static parsing.
- `internal/scan`: readiness inventory and findings.
- `internal/plan`: human-readable recovery plan.
- `internal/manifest`: portable static recovery facts.
- `internal/rehearse`: recovery-bundle verification, Docker Compose canonicalization, isolation rewrite, restore execution, checks, cleanup, evidence.
- `internal/safefs`: traversal-safe archive extraction.
- `internal/redact`: report redaction helpers.
- `internal/model`: stable internal data contracts.

## Why two Compose parsers?

Static `scan` needs to work without Docker, so v0.1 includes a deliberately small YAML subset parser for common Compose syntax. It identifies advanced YAML constructs and warns that the static result may be incomplete.

Mutation is different: safety cannot depend on a home-grown parser. `rehearse` requires Docker Compose v2 and asks Compose itself to render the canonical JSON model using `config --no-interpolate --no-env-resolution`. All isolation rewriting happens against that canonical structure.

## Rehearsal pipeline

1. Stage bundle read-only or extract a tar bundle using safe extraction.
2. Parse `composedrill-backup.json`.
3. Verify every declared artifact checksum.
4. Locate the checksummed Compose artifact and validate any adjacent `.env`/selected checks file before Docker Compose reads it.
5. Run Docker Compose canonicalization with interpolation and service env-file resolution disabled (`--no-interpolate --no-env-resolution`) and a scrubbed environment.
6. Reject unsafe runtime/namespace-sharing modes, host-capable volume/network driver configuration, daemon-side external logging, and host-file-loading features that cannot yet be bounded safely (`include`, `extends`, `label_file`); validate every recovered env/secret/config/checks file stays inside the bundle and is checksummed.
7. Rewrite the model:
   - unique project name
   - managed/run labels
   - production binds → run-local binds
   - named/external volumes → run-local named volumes
   - networks → run-local `internal` networks
   - host port publications removed; HTTP/TCP probes run from a tool-owned container on the verified internal recovery network
   - remove `container_name`
8. Restore mapped bind data.
9. Create disposable volumes and restore mapped volume archives.
10. Start declared database services and apply logical database dumps.
11. Start the complete stack.
12. Evaluate active Docker healthchecks and explicit checks, including optional expected-output markers for command checks; explicit checks are applied to every running replica of a scaled service.
13. Compute PASS / FAIL / MANUAL CHECK REQUIRED.
14. Label-scoped cleanup.
15. Persist redacted evidence outside the ephemeral workspace.

## Evidence state machine

A service has four dimensions:

- `configuration`
- `data_restore`
- `startup`
- `health`

Any service `FAIL`, cleanup failure, or HIGH/CRITICAL runner finding makes the overall run fail. Any unresolved `MANUAL CHECK REQUIRED` prevents PASS. This prevents a service from receiving a green result merely because its process is running.
