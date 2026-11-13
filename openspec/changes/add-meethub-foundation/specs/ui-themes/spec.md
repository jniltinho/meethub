# Spec Delta

## Purpose

Defines MeetHub's visual themes: the catalog (carried over from ad-manager plus MeetHub's own pair), the contract every
theme fulfils, how a user picks a theme and how the choice is kept.

## ADDED Requirements

### Requirement: Theme catalog
The system SHALL offer the themes `meethub-dark` (default), `meethub-light` and the ad-manager catalog (`default`,
`admanager`, `breeze`, `breeze-dark`, `violet`, `violet-dark`, `aurora`, `aurora-dark`, `crystal`, `crystal-dark`,
`cone`, `classic-blue`, `classic-gray`, `graphite`, `grafana`, `rabbitmq`, `matrix`, `winamp`, `pink-panther`,
`high-contrast`), each with an id, a label and a `light` or `dark` color scheme.

#### Scenario: Theme list from the API
- **WHEN** a client requests `GET /api/v1/themes`
- **THEN** it receives every theme's id, label and scheme, with `meethub-dark` marked as the default

### Requirement: Theme contract
Every theme SHALL define all role tokens of the contract (backgrounds, panels, surfaces, borders, text, muted text,
primary, danger, success, warning, focus, and the toast, alert and dialog roles), and SHALL keep square corners.

#### Scenario: Missing token detected
- **WHEN** the theme contract test runs over every theme file
- **THEN** it fails naming any theme that leaves a contract token undefined

#### Scenario: Square corners
- **WHEN** any theme is active
- **THEN** buttons, inputs, cards, toasts and dialogs render with a 0 px border radius

### Requirement: Contrast
Every theme SHALL meet WCAG AA contrast (4.5:1 for body text, 3:1 for large text, icons and focus indicators) for its
text and control roles, checked by an automated test.

#### Scenario: Contrast test
- **WHEN** the contrast test computes each theme's text-on-background pairs
- **THEN** every pair meets its AA threshold

### Requirement: Theme picker and persistence
Users SHALL choose a theme from a picker showing a preview of each theme; the choice SHALL apply immediately without
reload, be saved in their preferences on the server and follow them to other browsers.

#### Scenario: Theme follows the user
- **GIVEN** a user picked `classic-blue` in one browser
- **WHEN** they log in from another browser
- **THEN** the app renders with `classic-blue`

#### Scenario: Unknown theme id
- **WHEN** a client saves the theme preference `neon`
- **THEN** the response is 400 with code `invalid_theme`

### Requirement: Default theme before login
Pages shown before login SHALL use the organization's default theme (initially `meethub-dark`), and SHALL follow the
browser's `prefers-color-scheme` between `meethub-dark` and `meethub-light` when the organization default is
`meethub-dark`.

#### Scenario: Light system preference
- **GIVEN** the browser prefers a light color scheme and the organization default is `meethub-dark`
- **WHEN** an anonymous visitor opens the login page
- **THEN** it renders with `meethub-light`

### Requirement: No flash of the wrong theme
The SPA SHALL apply the remembered theme before the first paint.

#### Scenario: Reload with a saved theme
- **GIVEN** a user whose theme is `matrix`
- **WHEN** they reload the page
- **THEN** no frame is painted with another theme
