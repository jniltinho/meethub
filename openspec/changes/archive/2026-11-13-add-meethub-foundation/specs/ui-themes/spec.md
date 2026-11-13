# Spec Delta

## Purpose

Defines MeetHub's visual themes: the catalog (carried over from ad-manager plus MeetHub's own pair), the contract every
theme fulfils, how a theme is chosen before and after login, and how the choice is kept.

## ADDED Requirements

### Requirement: Theme catalog
The system SHALL offer 22 themes, each with an id, a label and a `light` or `dark` scheme: `meethub-dark` (default),
`meethub-light` and the ad-manager catalog `default`, `admanager`, `breeze`, `breeze-dark`, `violet`, `violet-dark`,
`aurora`, `aurora-dark`, `crystal`, `crystal-dark`, `cone`, `classic-blue`, `classic-gray`, `graphite`, `grafana`,
`rabbitmq`, `matrix`, `winamp`, `pink-panther`, `high-contrast`.

#### Scenario: Theme list from the API
- **WHEN** a client requests `GET /api/v1/themes`
- **THEN** it receives the 22 themes' ids, labels and schemes, with `meethub-dark` marked as the default

#### Scenario: Registry, styles and picker agree
- **WHEN** the theme lockstep test runs
- **THEN** every registry id has a `[data-theme='<id>']` style rule and a picker entry, and no style rule or picker
  entry exists without a registry id

### Requirement: Theme contract
Every theme SHALL resolve every required role token of the contract (backgrounds, panels, surfaces, borders, text,
muted text, primary, danger, success, warning, focus, and the toast, alert and dialog roles) to a valid color, either by
declaring it or by inheriting the contract default, and SHALL keep square corners.

#### Scenario: Unresolved token detected
- **WHEN** the browser contract test applies each theme and reads the computed value of every required token
- **THEN** it fails naming any theme and token that does not resolve to a color

#### Scenario: Square corners
- **WHEN** any theme is active
- **THEN** buttons, inputs, cards, toasts and dialogs render with a 0 px border radius

### Requirement: Contrast
Every theme SHALL meet WCAG AA contrast for a declared foreground/background matrix (4.5:1 for body text, 3:1 for
large text, icons, borders of inputs and focus indicators), measured on the composited colors in the browser.

#### Scenario: Contrast test
- **WHEN** the contrast test renders each theme and measures every pair in the matrix
- **THEN** every pair meets its AA threshold

### Requirement: MeetHub default palette
`meethub-dark` SHALL use the mockup palette: app background `#0f1322`, panel `#161b2e`, surface `#1d2338`, border
`#2a3150`, text `#e8eaf3`, muted text `#9aa1bd`, primary `#5b5fef`, success `#2fb170`, warning `#e5a531`, danger
`#e5484d`; `meethub-light` SHALL use the same primary hue on light neutrals.

#### Scenario: Primary button color
- **WHEN** the "Start meeting" style primary button renders in `meethub-dark`
- **THEN** its background is `#5b5fef`

### Requirement: Theme picker and persistence
Users SHALL choose a theme from a picker showing a preview of each theme; the choice SHALL apply immediately without
reload, be saved in their preferences on the server and follow them to other browsers.

#### Scenario: Theme follows the user
- **GIVEN** a user picked `classic-blue` in one browser
- **WHEN** they log in from another browser
- **THEN** the app renders with `classic-blue` after login

#### Scenario: Unknown theme id
- **WHEN** a client saves the theme preference `neon`
- **THEN** the response is 400 with code `invalid_theme`

### Requirement: Theme precedence
The active theme SHALL be, in order: the logged-in user's preference; else the theme last applied in this browser
while logged in, until logout; else the organization default, which the server renders into `index.html`; when the
organization default is `meethub-dark`, a browser preferring a light scheme SHALL get `meethub-light`. Logging out
SHALL clear the browser's remembered theme.

#### Scenario: Fresh browser
- **GIVEN** a browser that never opened MeetHub, preferring a dark scheme
- **WHEN** it opens the login page
- **THEN** the first paint already uses the organization default theme

#### Scenario: Light system preference
- **GIVEN** the browser prefers a light scheme and the organization default is `meethub-dark`
- **WHEN** an anonymous visitor opens the login page
- **THEN** it renders with `meethub-light`

#### Scenario: Shared browser after logout
- **GIVEN** a user with theme `matrix` logs out
- **WHEN** the login page shows next
- **THEN** it uses the organization default, not `matrix`

### Requirement: No flash of the wrong theme
The SPA SHALL apply the theme chosen by the precedence rules before the first paint, without inline scripts.

#### Scenario: Reload with a remembered theme
- **GIVEN** a logged-in user whose theme is `matrix`
- **WHEN** they reload the page
- **THEN** no frame is painted with another theme
