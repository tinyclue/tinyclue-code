# Team Shared Memory

## Team Information
- Team: Platform Engineering
- On-call rotation: weekly, start Monday
- Slack channel: #platform-eng
- Standup: Daily at 9:30 AM PT

## Shared Conventions
- Code review response time: within 4 hours during business hours
- Use @platform-eng for team-wide notifications
- All incidents must be documented in postmortem template

## Current Sprint (Sprint 24)
- Goal: Complete authentication service migration
- Deploy freeze: Every Friday 4 PM - Monday 6 AM PT
- Release manager: Alice (this sprint)

## Infrastructure
- Staging: staging-api.internal.company.com
- Production: api.company.com
- Grafana dashboard: grafana.internal/d/platform-eng
- Sentry project: platform-engineering-backend

## Common Commands
```bash
# Deploy to staging
make deploy-staging

# Run full test suite
make test-all

# Check service health
curl https://staging-api.internal.company.com/healthz
```
