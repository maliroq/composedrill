# Threat model

## Assets to protect

- production containers, networks, and volumes
- production bind-mounted files
- backup artifacts and credentials
- secrets in `.env`, Docker secrets/configs, and application configuration
- evidence integrity
- the host running Docker

## Threats considered

### Accidental production deletion

Mitigation: unique recovery project names, rewritten volume names and bind paths, exact run labels, deletion guards, no production `down -v` path.

### A restored workload contacts production/external systems

Mitigation: all recovery networks are forced internal, host publications are removed, and namespace-sharing/host-routing escape features are conservatively refused. This blocks normal routed egress but is not a hardened sandbox. A workload that requires external SaaS, SMTP, webhooks, or cloud APIs must use a safe mock or remain manual.

### Backup archive path traversal

Mitigation: bundle/bind extraction and named-volume archive validation reject absolute paths, parent traversal, links, and special-file entries before restore. Generic archive processing does not impose a global byte quota because legitimate backups can be large; a deliberately huge archive can still consume disk/CPU and should be handled on a capacity-limited test host.

### Backup maps to the wrong service/path

Mitigation: persistent state requires explicit bundle mapping by service/target or logical volume. Unknown mappings do not become PASS.

### Secret disclosure in reports

Mitigation: reports do not include normalized Compose contents or environment maps; common sensitive assignments and URL credentials in error text are redacted; backup path is reduced to its basename. Service env files and file-backed secrets/configs must stay inside the checksummed recovery bundle. Redaction is heuristic and not a DLP guarantee.

### Compose references undeclared host files

Mitigation: interpolation `.env` and selected checks files are validated before canonicalization; service env-file resolution is disabled during canonicalization; referenced env/secret/config files must resolve inside the recovery bundle and be checksummed; v0.1 refuses `include`, `extends`, and `label_file`.

### Malicious Compose file gains host control

Mitigation: privileged/device/GPU access, custom runtimes, namespace/cgroup sharing, `volumes_from`, capability additions, external links, `extra_hosts`, service `security_opt`, host-capable volume/network driver settings, and unknown mount types are refused. Bind mounts are rewritten and daemon-side logging is disabled. This does **not** make arbitrary untrusted images safe to execute: Docker itself is a privileged boundary. Run only backups/Compose definitions you trust.

### Malicious container image

Out of scope for sandboxing. ComposeDrill limits network and host-resource access but is not a hardened container sandbox. Use a dedicated test host/VM for untrusted images.

### Backup content exploits application parser

Out of scope. Restoring data means the application will parse it. Use isolated infrastructure for hostile/untrusted backup data.

## Trust boundaries

- Docker daemon: trusted and highly privileged.
- Docker Compose CLI: trusted canonicalizer/executor.
- recovery bundle: trusted enough to restore, but validated for path/checksum integrity.
- application images: assumed trusted by the operator.
- production state: never a restore target.

### Integrity versus authenticity

Bundle SHA-256 values detect accidental corruption and establish what was validated before the rehearsal, but the manifest is not signed in v0.1. Directory-form bundles are consumed in place after validation, so concurrent mutation is outside the v0.1 integrity guarantee; use an immutable/tar-staged bundle to avoid that race. An attacker who can rewrite both an artifact and its manifest can create matching checksums. Signed evidence/manifests are a later hardening target.

### Docker daemon-side escape channels

Mitigation: automatic rehearsal refuses custom/non-local volume drivers, network driver options/non-bridge drivers, GPU/custom-runtime/cgroup/credential-spec escape surfaces, and service `security_opt`; service logging is rewritten to the `none` driver so syslog/fluentd-style drivers cannot bypass the internal container network through the daemon. Unknown non-bind/volume/tmpfs mount types are refused.
