# Contributing

Safety and honest recovery status outrank feature breadth. A contribution must not turn an UNKNOWN/MANUAL state into PASS merely to improve UX.

## Start here: add a real recovery fixture

A high-value first contribution is:

> **Add a recovery fixture for a real self-hosted stack.**

Use synthetic data only. Do not submit real secrets, production paths, personal backups, or proprietary application data.

A fixture proposal should include:

- a minimal Compose file that preserves the real persistence/recovery pattern;
- a checksummed `composedrill-backup.json` recovery bundle;
- the application-consistent backup method used to create the artifact;
- at least one check that proves restored state rather than only process startup;
- the expected outcome (`PASS` or an intentional, explained `FAIL`);
- cleanup/isolation assertions when the fixture introduces a new Docker resource pattern;
- a short note explaining what real recovery mistake the fixture would catch.

Good examples include: an application with a named volume plus a database, a bind-mounted config dependency, a one-shot migration that must remain manual, or a restore that should fail because required state is missing.

A fixture is not accepted merely because `docker compose up` succeeds.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -buildvcs=false ./cmd/composedrill
```

Docker-backed changes must also run:

```bash
go test -tags=integration -count=1 -v ./internal/integration
```

For the complete local release gate on macOS:

```bash
python3 dev.py
```

## Pull requests

Please include:

- the failure/recovery behavior being changed;
- tests for the success path and at least one meaningful failure path;
- any new destructive Docker operation and its ownership guard;
- documentation for changed recovery semantics;
- no real secrets, production paths, or backup data in fixtures.

Do not add dependencies casually. The v0.1 CLI intentionally builds with the Go standard library only.
