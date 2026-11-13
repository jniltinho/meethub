# Spec Delta

## Purpose

Defines how the SPA tells users about results and asks for decisions: themed toasts, inline alerts and dialogs that
replace the browser's native `alert()`, `confirm()` and validation bubbles in every theme.

## ADDED Requirements

### Requirement: Toasts
The SPA SHALL show toasts of kind `success`, `info`, `warning` and `error` stacked in the top-right corner, each with a
title, message, icon and close button; they SHALL hide after 4 s (errors after 8 s), pause while hovered or focused and resume with
the remaining time, and be announced to screen readers.

#### Scenario: Success toast
- **WHEN** a user saves their profile successfully
- **THEN** a `success` toast appears, then disappears after 4 seconds

#### Scenario: Hover pauses dismissal
- **WHEN** the pointer stays over a toast past its timeout
- **THEN** the toast stays until the pointer leaves

#### Scenario: Screen reader
- **WHEN** an `error` toast appears
- **THEN** it is in an `aria-live="assertive"` region; other kinds use `polite`

#### Scenario: Stack limit
- **WHEN** more than 5 toasts are visible
- **THEN** the oldest is removed

### Requirement: Inline alerts
The SPA SHALL provide inline alert banners of kind `info`, `success`, `warning` and `error`, with optional title,
action and dismiss button, for messages tied to a page or form.

#### Scenario: Form-level error
- **WHEN** the login API returns 401 `invalid_credentials`
- **THEN** an `error` alert above the form shows the translated message

### Requirement: Dialogs
The SPA SHALL provide promise-based dialogs: alert (OK), confirm (OK/Cancel, resolves true/false) and typed confirm
(the confirm button enables only when the typed text matches the expected value, case-insensitive and trimmed).

#### Scenario: Confirm before disabling a user
- **WHEN** an admin clicks Disable on a user
- **THEN** a confirm dialog asks for confirmation and the user is disabled only if they choose OK

#### Scenario: Typed confirm
- **GIVEN** a typed confirm expecting `ana@example.com`
- **WHEN** the admin types `ANA@example.com `
- **THEN** the confirm button becomes enabled

### Requirement: Dialog accessibility
Dialogs SHALL be modal, trap focus, start focus on the safest action, close with Escape as a cancel, restore focus to
the element that opened them, and carry `role="dialog"` or `role="alertdialog"` with a labelled title.

#### Scenario: Overlapping dialogs
- **WHEN** code opens a confirm dialog while another dialog is open
- **THEN** the second waits in a queue and opens after the first resolves; neither promise is lost

#### Scenario: Session ends with a dialog open
- **WHEN** the user logs out or the session expires while a confirm dialog is open or queued
- **THEN** every pending confirm resolves `false`, every alert resolves, and no confirmed action runs

#### Scenario: Escape cancels
- **WHEN** a confirm dialog is open and the user presses Escape
- **THEN** the dialog closes, the promise resolves `false` and focus returns to the opening button

### Requirement: No native browser UI
The SPA SHALL NOT call `window.alert`, `window.confirm` or `window.prompt`, and SHALL replace native form validation
bubbles with a themed invalid state and message.

#### Scenario: Lint guard
- **WHEN** the frontend check runs
- **THEN** it fails if the source calls `alert(`, `confirm(` or `prompt(` on `window`

#### Scenario: Required field
- **WHEN** the user submits the login form with an empty e-mail
- **THEN** the field shows the theme's invalid border and message and no browser bubble appears

### Requirement: Themed in every theme
Toasts, alerts and dialogs SHALL take their colors only from theme role tokens so every theme in the catalog styles
them.

#### Scenario: Gallery check
- **WHEN** the visual test renders the feedback gallery page in each theme
- **THEN** every toast, alert and dialog kind is drawn with that theme's tokens and passes the contrast test
