# Roadmap

This is a direction list, not a commitment or release schedule. Items move only if the recovery-safety case remains clear.

## v0.1 — recovery rehearsal core

- Compose discovery and static readiness scan
- recovery plan + manifest
- checksummed recovery-bundle contract
- isolation rewrite
- bind and named-volume restore
- PostgreSQL and MariaDB logical restore paths; MySQL shares the implementation path but is not independently release-gated
- Docker healthchecks + HTTP/TCP/command/DNS/file checks
- label-scoped cleanup and evidence
- disposable integration fixtures

## v0.2 — backup ecosystem adapters

- Restic snapshot → recovery bundle adapter
- Kopia snapshot → recovery bundle adapter
- Duplicati restore-test/bundle adapter where practical
- Backrest/Restic workflow documentation
- automatic image digest capture when daemon access is available
- safely bounded Compose override/include/extends/label-file fidelity improvements
- explicit profile-selection semantics and one-shot/init-service modeling
- richer SQLite verification
- recovery point age/RPO checks
- scheduled CI examples

## Later

- signed evidence reports
- narrowly scoped application recipes where recovery semantics can be tested
- optional TUI only if it improves drill clarity without hiding evidence
