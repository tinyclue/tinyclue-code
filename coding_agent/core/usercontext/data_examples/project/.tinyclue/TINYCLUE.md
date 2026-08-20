# Project .tinyclue Configuration

## Build System
- Use Makefile for build automation
- Targets: `build`, `test`, `lint`, `clean`, `run`
- Use `go build -ldflags="-s -w"` for production builds
- Cross-compile with `GOOS=linux GOARCH=amd64` for deployment

## CI/CD Pipeline
- GitHub Actions for CI
- Run `make lint` and `make test` on every PR
- Run security scan on dependency updates
- Deploy to staging on merge to `develop`
- Deploy to production on tag push (`v*`)

## Deployment
- Docker multi-stage builds for container images
- Health check endpoints: `/healthz`, `/readyz`
- Graceful shutdown with 30s timeout
