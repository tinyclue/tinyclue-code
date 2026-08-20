# Security Policy Rules

## Authentication
- Use OAuth 2.0 / OIDC for user authentication
- Implement rate limiting on all authentication endpoints (max 5 attempts per minute)
- Session tokens must expire within 15 minutes
- Use HTTP-only, Secure, SameSite cookies for session management

## Input Validation
- Validate all user input on both client and server side
- Use parameterized queries for all database operations
- Sanitize all output to prevent XSS attacks
- Implement CSRF protection on all state-changing endpoints

## Dependency Management
- Pin all dependency versions in lock files
- Run `npm audit` or equivalent before each release
- No deprecated packages with known CVEs
