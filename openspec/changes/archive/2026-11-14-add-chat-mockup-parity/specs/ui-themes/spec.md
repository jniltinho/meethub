# Spec Delta

## MODIFIED Requirements

### Requirement: Theme contract
Every theme SHALL resolve every required role token of the contract (backgrounds, panels, surfaces, borders, text,
muted text, primary, danger, success, warning, focus, and the toast, alert and dialog roles) to a valid color, either by
declaring it or by inheriting the contract default, and SHALL use the shared corner radii, which no theme overrides:
4 px for small elements, 6 px for controls and 8 px for containers.

#### Scenario: Unresolved token detected
- **WHEN** the browser contract test applies each theme and reads the computed value of every required token
- **THEN** it fails naming any theme and token that does not resolve to a color

#### Scenario: Square corners
- **WHEN** any theme is active
- **THEN** no button, input, card, toast or dialog renders with a 0 px border radius: square corners are no longer
  part of the contract

#### Scenario: Subtle corners
- **WHEN** any theme is active
- **THEN** buttons and inputs render with a 6 px border radius, cards, toasts and dialogs with 8 px, and nothing but
  presence dots, counters and round pictures has a larger one

### Requirement: Contrast
Every theme SHALL meet WCAG AA contrast for a declared foreground/background matrix (4.5:1 for body text, 3:1 for
large text, icons, borders of inputs and focus indicators), measured on the composited colors in the browser; and
`meethub-dark`, `meethub-light` and `high-contrast` SHALL meet WCAG AAA on the same matrix (7:1 for body text, 4.5:1
for large text).

#### Scenario: Contrast test
- **WHEN** the contrast test renders each theme and measures every pair in the matrix
- **THEN** every pair meets its AA threshold

#### Scenario: AAA themes
- **WHEN** the contrast test renders `meethub-dark`, `meethub-light` and `high-contrast`
- **THEN** every text pair of the matrix, muted text and text on primary buttons included, reaches 7:1

### Requirement: MeetHub default palette
`meethub-dark` SHALL use the AAA palette of the approved layout: app background `#0c1020`, sidebar `#10152a`, panel
`#151b31`, surface `#1d2442`, border `#2c3458`, text `#f2f4fb`, muted text `#b9c0d8`, primary `#3d41cf` with white
text, accent text `#b3b8ff`, success `#4ade80`, warning `#fbbf24`, danger text `#fca5a5` and danger fill `#9b1c1c`;
`meethub-light` SHALL use the same primary hue on light neutrals.

#### Scenario: Primary button color
- **WHEN** the "Start meeting" style primary button renders in `meethub-dark`
- **THEN** its background is `#3d41cf` and its white label has a contrast of at least 7:1

## ADDED Requirements

### Requirement: Quick theme switcher
The sidebar's foot SHALL offer a quick switcher with `meethub-dark`, `meethub-light`, `classic-blue` and `matrix`,
each one click away with the same effect as the full picker, and a link to the picker with all the themes.

#### Scenario: One click
- **GIVEN** a user on `meethub-dark`
- **WHEN** they open the quick switcher and choose the light theme
- **THEN** the app turns to `meethub-light` at once and the choice is saved in their preferences

#### Scenario: All themes
- **WHEN** the user follows the switcher's "All themes" link
- **THEN** Settings opens on Appearance with the full picker
