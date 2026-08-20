# Project TINYCLUE.md

## Project Overview
This is a Go backend service implementing a RESTful API for user management.

## Architecture
- Use clean architecture / hexagonal architecture pattern
- Dependency injection via constructor functions
- Repository pattern for data access
- Middleware chain for HTTP request processing

## Language-Specific Rules (Go)
- Use `errors.New` for simple errors, `fmt.Errorf` with `%w` for wrapped errors
- Always handle returned errors; use `_ =` only when explicitly discarding
- Use `context.Context` as first parameter in all public functions
- Export only what's necessary (capitalize public types/functions)
- Use table-driven tests with `t.Run()` subtests
- Follow `gofmt` formatting (run `gofmt -s -w` before committing)

## Naming Conventions
- REST endpoints: lowercase, kebab-case (`/api/v1/user-profiles`)
- Database tables: snake_case, plural (`user_profiles`)
- JSON fields: camelCase (`firstName`)
- Environment variables: UPPER_SNAKE_CASE (`DATABASE_URL`)
