# Backup and recovery semantics

ComposeDrill distinguishes **artifact integrity**, **restore completion**, and **application recovery**.

## File and volume data

A checksummed artifact proves the bytes matched the bundle manifest when validation ran. Directory-artifact hashes use a versioned, length-framed path/content encoding to avoid ambiguous concatenation. It does not prove those bytes were captured consistently from a live application.

For ordinary bind-mounted files, a directory/tar artifact can be restored to a disposable bind path. Named and anonymous volume tar archives are validated for traversal/links/special files, then restored only into disposable volumes through a pinned, network-disabled utility container.

## Databases

Live database files are special. A filesystem-level archive may be unusable even if every byte is intact.

Preferred v0.1 strategies:

- PostgreSQL: `pg_dump`/`pg_dumpall` SQL or custom-format logical dump; restore through `psql`/`pg_restore` inside a version-compatible recovery container.
- MariaDB: `mariadb-dump` logical SQL; restore through the database client inside the recovery container. This path is included in the Docker release gate.
- MySQL: `mysqldump` logical SQL uses the same `mysql-sql` implementation path, but MySQL is not independently Docker-gated in v0.1.
- SQLite: a backup produced with SQLite's online-backup API or `sqlite3 .backup`, then included in the mapped bind artifact.

ComposeDrill does not create hot raw database archives and call them safe.

## Recovery bundle contract

The manifest is `composedrill-backup.json`:

```json
{
  "format": "composedrill-recovery-bundle",
  "version": 1,
  "created_at": "2026-10-26T14:00:00Z",
  "compose_sha256": "...",
  "entries": []
}
```

Entry kinds currently understood by the runner:

- `compose`
- `bind`
- `volume` (named volume by `volume`, or anonymous volume by `service` + `target`)
- `database`
- metadata-only verified entries such as `env`, `secret`, `config`, and `checks`

Every entry must use a relative artifact path and SHA-256 checksum. Any service `env_file`, file-backed Compose secret/config, adjacent interpolation `.env`, or selected checks file consumed by rehearsal must also be present as a declared manifest entry; undeclared recovery inputs are refused.

## Existing backup ecosystems

v0.1 deliberately defines the **ingress contract** instead of embedding every storage engine. A Restic/Kopia/Duplicati/rsync workflow can restore or export the relevant snapshot into a temporary directory and then produce a ComposeDrill bundle manifest. Native adapters are planned after the core isolation/recovery semantics have real-world validation.

## Manifest validation

v0.1 refuses unknown entry kinds, malformed SHA-256 values, ambiguous duplicate recovery mappings, and manifests without exactly one `compose` entry. Explicit checks must be declared with kind `checks`; a merely checksummed file of another kind is not accepted as executable recovery validation logic.
