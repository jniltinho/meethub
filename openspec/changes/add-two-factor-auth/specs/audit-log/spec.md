# Spec Delta

## ADDED Requirements

### Requirement: Two-factor is audited
Enrolling, turning off, an administrator's reset, a recovery code used and a voided ticket SHALL each record an
audit event with the actor and the target, never a code or a secret.

#### Scenario: Reset recorded
- **WHEN** an admin resets Bruno's two-factor
- **THEN** an event `user.two_factor_reset` names the admin and Bruno
