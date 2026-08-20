# Backend Development Rules

## Payment Processing
- Always validate webhook signatures before processing
- Use idempotency keys for all payment operations
- Implement circuit breaker pattern for external API calls
- Cache billing plans and pricing data (refresh every 5 minutes)

## Database
- Use serializable isolation level for financial transactions
- Implement optimistic locking for concurrent updates
- Audit log all changes to financial records
- Partition invoice table by month for performance

## Error Handling
- Map payment gateway errors to user-friendly messages
- Never expose internal error details to clients
- Implement dead-letter queue for failed webhook events
