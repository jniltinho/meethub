# Spec Delta

## ADDED Requirements

### Requirement: Meeting screens in the contrast matrix
The contrast matrix SHALL include every color pair of the meeting screens: the room's stage, tile name labels and
marks over video, the speaking outline, the control bar and its pressed, off and leave states, control tooltips, the
side panel, the pre-join screen and its level meter, the details room card, the "In the room" tag and the sidebar's
room count. They SHALL meet AA in every theme and AAA in `meethub-dark`, `meethub-light` and `high-contrast`.

#### Scenario: Name label over video
- **WHEN** the contrast test measures a tile's name label in `meethub-dark`
- **THEN** its text reaches 7:1 against the label's own opaque background, whatever the video behind it

#### Scenario: Control bar in a light theme
- **WHEN** the contrast test measures the control bar's icons in `meethub-light`
- **THEN** every icon and its pressed, off and leave states reach at least 3:1, and their tooltips' text 7:1
