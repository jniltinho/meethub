# Spec Delta

## ADDED Requirements

### Requirement: Single sign-on on the login page
With a provider configured the login page SHALL show a "Sign in with <name>" button; with password login off it
SHALL show only that button. A failed single sign-on SHALL return to the login page with a themed, generic message.

#### Scenario: Both ways offered
- **GIVEN** a provider named "Acme ID" and password login on
- **WHEN** the login page opens
- **THEN** it shows the password form and a "Sign in with Acme ID" button

#### Scenario: Failure
- **WHEN** a single sign-on attempt is refused
- **THEN** the login page says the sign-in could not be completed, without saying why
