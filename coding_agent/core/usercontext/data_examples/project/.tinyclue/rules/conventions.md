# Coding Conventions

## HTTP Handlers
- Use standard `net/http` handlers with `http.Handler` interface
- Request validation in middleware layer
- Structured JSON responses with envelope: `{"data": ..., "error": ...}`
- Pagination via query params: `?page=1&limit=20`

## Database
- Use `database/sql` with `pgx` driver for PostgreSQL
- All queries must use parameterized placeholders ($1, $2, etc.)
- Migrations with `golang-migrate/migrate`
- Index all foreign key columns

## Logging
- Use structured logging with `slog`
- Log levels: DEBUG, INFO, WARN, ERROR
- Include request ID in every log entry
- Never log PII or sensitive data
