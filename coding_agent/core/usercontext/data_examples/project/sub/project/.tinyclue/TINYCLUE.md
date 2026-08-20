# Sub-Project .tinyclue Configuration

## Dependencies
- Stripe Go SDK for payment processing
- `shopspring/decimal` for monetary calculations
- `go-redis/cache` for rate limiting

## Module Structure
```
billing/
├── handler/       # HTTP handlers
├── service/       # Business logic
├── repository/    # Data access
├── model/         # Domain models
└── webhook/       # Webhook processing
```

## Testing
- Mock Stripe API for payment tests
- Use golden files for invoice template tests
- Load test with 1000 concurrent requests for rate limiting validation
