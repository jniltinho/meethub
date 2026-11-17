# Delta for ui-themes

## MODIFIED Requirements

### Requirement: Theme contract
Every theme SHALL resolve every required role token of the contract (backgrounds, panels, surfaces, borders, text,
muted text, primary, danger, success, warning, focus, and the toast, alert and dialog roles) to a valid color, either by
declaring it or by inheriting the contract default, and SHALL use the shared corner radii, which no theme overrides:
2 px for small elements, 3 px for controls and 4 px for containers.

#### Scenario: Unresolved token detected
- **WHEN** the browser contract test applies each theme and reads the computed value of every required token
- **THEN** it fails naming any theme and token that does not resolve to a color

#### Scenario: Square corners
- **WHEN** any theme is active
- **THEN** no button, input, card, toast or dialog renders with a 0 px border radius: square corners are no longer
  part of the contract

#### Scenario: Subtle corners
- **WHEN** any theme is active
- **THEN** buttons and inputs render with a 3 px border radius, cards, toasts and dialogs with 4 px, and nothing but
  presence dots, counters and round pictures has a larger one
