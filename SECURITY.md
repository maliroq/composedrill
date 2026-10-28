# Security policy

## Reporting

Please report vulnerabilities privately to the repository maintainers using GitHub Security Advisories once the public repository is created. Do not open a public issue for a vulnerability that could cause production data deletion, secret disclosure, or host escape.

## Security-sensitive areas

Treat changes to these areas as high risk:

- cleanup ownership checks
- bind/volume isolation rewrites
- archive extraction
- secret/config handling
- host/privileged runtime refusal
- shell/database restore invocation
- report redaction

See `docs/SAFETY.md` and `docs/THREAT_MODEL.md`.
