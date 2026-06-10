# Security Policy

## Scope

tokless modifies agent configuration files (Claude Code, OpenCode, Codex)
and downloads tool binaries from their official sources. Security issues in
these areas are in scope.

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do not** open a public GitHub issue.
2. Email the maintainer at the address listed in the repository profile,
   or use GitHub's private vulnerability reporting feature under the
   **Security** tab of this repository.
3. Include steps to reproduce, the affected version, and any potential impact.

You should receive an acknowledgment within 48 hours. We aim to release a
fix within 7 days of confirmation.

## Supported Versions

Only the latest release is supported with security updates.

| Version | Supported |
| ------- | --------- |
| latest  | Yes       |
| < latest | No       |

## Security Considerations

- tokless downloads binaries from official GitHub releases and npm registries.
  Verify checksums when possible.
- Config files are written with mode 0644. Sensitive tokens should not be
  stored in tokless-managed config files.
- The `install.sh` and `install.ps1` scripts are fetched over HTTPS from
  this repository's main branch.
