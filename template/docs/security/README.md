# Security procedures

- Vulnerability reporting: see [SECURITY.md](../../SECURITY.md).
- Dependencies: Dependabot alerts and security updates are enabled;
  dependency review runs on every pull request.
- Workflows pin GitHub Actions to full-length commit SHAs.
- Secrets: never commit them; secret scanning with push protection is
  enabled on this repository.
