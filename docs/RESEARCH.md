# Market and competitor research — 2026-10-26

## Conclusion

The broad idea “verify that backups restore” is **not new**. The useful wedge is narrower:

> **Docker Compose state discovery + explicit persistent-state mapping + isolated whole-stack restore + service-level checks + proof.**

That remains meaningfully distinct from the current tools reviewed below.

## Current landscape

| Project / ecosystem | What it does well | Gap relative to ComposeDrill |
|---|---|---|
| Restic | encrypted file backup, repository checks, full-data checks, file restore | does not reconstruct/boot a Compose application or verify service health |
| Kopia | snapshots, restore, snapshot verification including optional full file download/decrypt/decompress | file/snapshot validity, not Compose recovery semantics |
| Backrest | excellent Restic UI/orchestration, scheduling, restore browsing, hooks | primarily backup orchestration; not generic application recovery rehearsal |
| Duplicati | backup/restore UI; Sept. 2026 Canary added `restore-test` that restores a sample of files to a temp folder | sample file restore does not prove a Compose stack, database, secrets, dependencies, and health recover together |
| offen/docker-volume-backup | recurring Docker volume archives and documented restore procedure | restores volume data but leaves application-specific recovery/test orchestration to the user |
| restoredrill | strong PostgreSQL/MySQL throwaway-container restore proof and checks | database-centric, not Compose whole-stack discovery/isolation |
| RestoreProof | commercial runner/control-plane product with database/filesystem probes and signed evidence | broader backup verification service; not an OSS Compose-state scanner/rehearsal CLI |
| RestoreLab | whole-workload recovery drills with isolation and checks | focused on Proxmox VE VM recovery rather than Docker Compose state |

## User pain evidence

Recent self-hosting discussions repeatedly show the same failure mode: backup jobs are green but restores are untested or manual. In August 2026, one r/selfhosted thread described “successful” PostgreSQL backups that were effectively empty because the source container was not running; commenters described custom throwaway-container restore pipelines rather than a standard tool. Other 2026 threads ask how to verify that Docker volumes are even covered by Restic or admit full restore drills are infrequent/time-consuming.

## Naming

`FireDrill` was rejected because it is already occupied by:

- FourCoreLabs `firedrill`, a Go malware/security-control simulation project.
- `firedrill` on PyPI, a current PostgreSQL backup restoration verifier.
- `firedrill.run`, an active AI-agent simulation product/CLI.

`RestoreDrill`, `RestoreProof`, and `RestoreLab` are also occupied in adjacent recovery-verification space.

Fresh web, GitHub-indexed, and general trademark-term searches on 2026-10-26 returned no obvious collision for **ComposeDrill**. Search absence is not clearance; this is a practical pre-launch screen, not a legal trademark opinion.

## Sources

- Restic docs: https://restic.readthedocs.io/en/stable/
- Kopia consistency docs: https://kopia.io/docs/advanced/consistency/
- Backrest: https://github.com/garethgeorge/backrest
- Duplicati docs: https://docs.duplicati.com/getting-started/restoring-files
- Duplicati 2.4.0.102 restore-test announcement: https://forum.duplicati.com/t/release-2-4-0-102-canary-2026-09-25/22729
- offen/docker-volume-backup: https://github.com/offen/docker-volume-backup
- restoredrill: https://github.com/ahmadpiran/restoredrill
- RestoreProof: https://restoreproof.io/
- RestoreLab: https://github.com/restorelab/restorelab
- Docker Compose CLI/config docs: https://docs.docker.com/reference/cli/docker/compose/
