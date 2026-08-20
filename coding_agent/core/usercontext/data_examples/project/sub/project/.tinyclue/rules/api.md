# API Design Rules

## RESTful Conventions
- Use plural nouns for resource endpoints (`/invoices`, `/payments`)
- HTTP methods: GET (read), POST (create), PUT (replace), PATCH (update), DELETE (remove)
- Return proper HTTP status codes (200, 201, 204, 400, 401, 403, 404, 409, 422, 500)
- Pagination: `page` and `limit` query parameters, response includes `total`, `page`, `limit`

## Request/Response Format
```json
{
    "data": { ... },
    "meta": {
        "page": 1,
        "limit": 20,
        "total": 100
    },
    "error": null
}
```

## Error Response Format
```json
{
    "data": null,
    "error": {
        "code": "VALIDATION_ERROR",
        "message": "Invalid input",
        "details": [
            {"field": "email", "message": "must be a valid email address"}
        ]
    }
}
```
