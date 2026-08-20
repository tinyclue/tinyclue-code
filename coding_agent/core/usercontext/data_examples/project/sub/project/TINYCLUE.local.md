# Sub-Project Local Overrides

## Local Development
- Billing module running on port 8081
- Stripe API in test mode (sk_test_* keys)
- Webhook forwarding via ngrok (ngrok http 8081)
- Mock payment gateway enabled

## Debug Settings
- PAYMENT_DEBUG=true — log full payment request/response
- SKIP_WEBHOOK_VERIFICATION=false — always verify (even in dev)
- SIMULATE_PAYMENT_FAILURE=false — toggle for testing failure paths

## WIP Notes
- Implementing invoice PDF generation
- Need to add rate limiting for webhook endpoints
- TODO: Write migration for billing_address column
