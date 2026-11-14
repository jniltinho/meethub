# Spec Delta

## ADDED Requirements

### Requirement: Single sign-on is audited
A successful single sign-on, a refused one and an account created by single sign-on SHALL each record an audit
event with the e-mail and the client address, never a token.

#### Scenario: Account created
- **WHEN** a first single sign-on creates an account
- **THEN** events `user.created` (by single sign-on) and `auth.oidc_login` are recorded
