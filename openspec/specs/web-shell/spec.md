# web-shell Specification

## Purpose
Defines the browser application's frame: setup and login pages, the authenticated layout every feature lives in,
navigation, responsiveness and keyboard accessibility.

## Requirements

### Requirement: Setup and login pages
The SPA SHALL read `/api/v1/bootstrap` and show the setup page (asking for the setup token) while setup is required,
the login page to anonymous visitors otherwise, and SHALL return the visitor to the same-origin route they asked for
after logging in.

#### Scenario: Deep link while logged out
- **GIVEN** an anonymous visitor on an install with users
- **WHEN** they open `/settings/appearance`
- **THEN** the login page is shown, and after a successful login they land on `/settings/appearance`

#### Scenario: Open redirect refused
- **WHEN** the login page is opened with a return target of `https://evil.example/`
- **THEN** after login the user lands on the home route

#### Scenario: Session expires while using the app
- **WHEN** any API call returns 401 `session_expired`
- **THEN** the SPA shows a toast saying the session expired and goes to the login page

### Requirement: Application layout
The authenticated layout SHALL have four regions: a 64 px navigation rail (Activity, Conversations, Teams, Meetings,
Files, Administration), a 264 px context sidebar, a fluid content area and a 320 px collapsible side panel.

#### Scenario: Administration hidden from members
- **GIVEN** a logged-in `member`
- **WHEN** the layout renders
- **THEN** the navigation rail has no Administration entry

#### Scenario: Side panel toggles
- **WHEN** the user closes the side panel
- **THEN** the content area widens and the panel's open state is remembered for the session

### Requirement: Responsive layout
Below 768 px the context sidebar SHALL become a drawer opened from a header button and the navigation rail SHALL
become a bottom bar, without horizontal page scrolling at 360 px width.

#### Scenario: Phone width
- **GIVEN** a 360 px wide viewport
- **WHEN** the user opens the app
- **THEN** the sidebar is hidden behind a menu button and the page does not scroll horizontally

### Requirement: Placeholder sections
Conversations, Teams, Meetings, Files and Activity SHALL have routes rendering an empty state that names the section,
so later changes replace content without changing navigation.

#### Scenario: Meetings route
- **WHEN** the user selects Meetings in the rail
- **THEN** the URL is `/meetings` and an empty state titled "Meetings" is shown

### Requirement: Settings and administration pages
The SPA SHALL provide Settings (profile, password, appearance, language) for every user, and Administration (users,
audit log) for owners and admins.

#### Scenario: Admin lists users
- **WHEN** an admin opens Administration → Users
- **THEN** a table lists users with name, e-mail, role and status, with actions to create, disable and change role

### Requirement: Keyboard and screen-reader access
Every interactive element SHALL be reachable by keyboard with a visible focus indicator, icon-only buttons SHALL have
accessible names, and route changes SHALL move focus to the new page's heading.

#### Scenario: Icon button name
- **WHEN** a screen reader reaches the navigation rail's Meetings button
- **THEN** it announces "Meetings"
