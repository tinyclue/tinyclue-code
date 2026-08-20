# Managed Organization Policy

These rules are managed by the organization and apply to all projects.

## Security Requirements
- All code must pass security review before deployment
- Use approved authentication libraries only
- No hardcoded secrets, API keys, or tokens
- All data at rest must be encrypted using AES-256
- All data in transit must use TLS 1.2 or higher

## Compliance
- All dependencies must be scanned for known vulnerabilities
- Maintain SBOM (Software Bill of Materials) for all releases
- Follow OWASP Top 10 guidelines for web applications

## Code Review
- Every PR requires at least one approved review
- Security-sensitive changes require two reviews
- All PRs must pass CI/CD pipeline checks

<!-- Note: The following standards are enforced by CI pipeline -->

## Project Standards
- Follow the organization's coding standards for each language
- Maintain at least 80% test coverage
- Use semantic versioning for all releases

@./rules/security-policy.md
@./rules/compliance.md
