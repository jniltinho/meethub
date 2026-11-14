# Spec Delta

## ADDED Requirements

### Requirement: OpenID Connect settings
The configuration SHALL have an `oidc` section (`issuer`, `client_id`, `name`, `scopes`, `auto_create`,
`allowed_domains`, `default_role`) with the client secret only from `MEETHUB_OIDC_CLIENT_SECRET`, and
`auth.password_login`. Validation SHALL reject an issuer that is not `https://`, a missing client id or secret, a
`default_role` above `member`, and password login off without a provider.

#### Scenario: Passwords off without a provider
- **WHEN** `auth.password_login = false` and no `oidc.issuer` is set
- **THEN** startup fails naming `auth.password_login`

#### Scenario: Secret in the file
- **WHEN** `config.toml` contains the OIDC client secret
- **THEN** startup fails saying secrets come only from the environment
