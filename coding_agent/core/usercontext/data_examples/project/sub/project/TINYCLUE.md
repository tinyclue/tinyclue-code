# Sub-Project TINYCLUE.md

This is a nested sub-project within the main project.

## Sub-Project Overview
Implements the billing and payment processing module.

## Module-Specific Rules
- All monetary values must use `decimal.Decimal` (not float64)
- Transactional integrity is critical: use database transactions
- Retry mechanism for payment gateway calls (max 3 retries, exponential backoff)
- Log all financial operations for audit trails
- Webhook handlers must be idempotent

## API Endpoints
- `POST /api/v1/billing/invoices` - Create invoice
- `GET /api/v1/billing/invoices/{id}` - Get invoice
- `POST /api/v1/billing/payments` - Process payment
- `POST /api/v1/billing/webhooks/stripe` - Stripe webhook receiver
