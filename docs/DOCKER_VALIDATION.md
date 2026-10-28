# Local Docker validation

ComposeDrill ships a deterministic macOS release-gate harness that runs entirely from the repository on the local machine.

From the extracted repository root, run exactly:

```bash
python3 dev.py
```

The harness detects macOS/architecture, Python, Go, Docker, Docker Compose v2, and the Docker daemon. It never installs system software. If Docker is unavailable, it writes a failed report with the exact prerequisite instead of treating skipped integration tests as success.
The Docker-backed gate requires the fixture and pinned utility images to be available locally or reachable from their container registry. This is the only network prerequisite; the harness does not install Docker, Go, Python, or other system software.


When prerequisites are available it runs:

```bash
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -o artifacts/composedrill-validation-bin ./cmd/composedrill
go test -tags=integration -count=1 -v ./internal/integration
```

The tagged integration suite must run every expected test with zero skips/failures. Besides the five public fixtures, it includes generated Docker cases for named-volume archive restore, anonymous-volume archive restore, and MariaDB logical restore with a row-content assertion. The harness then executes the stateless, PostgreSQL, SQLite, and bind-mount public rehearsals through the built CLI and requires each recovery evidence file to report overall `PASS`, cleanup `PASS`, and all service phases `PASS`.

The intentionally broken fixture is stricter: a non-zero process exit is not enough. The harness verifies the fixture contract (production has `/data/required.txt`, the recovery artifact omits it, the bind artifact maps to `/data`, and the healthcheck requires that file), then requires recovery evidence showing configuration `PASS`, data restore `PASS`, container startup `PASS`, application health `FAIL`, and an unhealthy/application-health reason. Any earlier crash, restore failure, cleanup failure, or unrelated error is classified `WRONG FAILURE`; a green recovery is `UNEXPECTED PASS`.

Before and after validation, the harness hashes the complete `examples/` fixture tree to prove the production fixtures were not mutated. It also audits Docker containers, networks, and volumes for ComposeDrill-managed/namespaced leftovers. It does not automatically delete pre-existing resources.

All command exit codes and redacted output are recorded in:

- `artifacts/local_verification_report.txt`
- `artifacts/local_verification_report.json`
- `artifacts/full_validation.log`

`dev.py` exits `0` only when the complete release gate passes and the broken fixture is classified `EXPECTED FAIL`. Otherwise it exits non-zero.
