## ADDED Requirements

### Requirement: Presenting banner
While the user shares their screen, the room SHALL show above the stage a banner saying they are presenting, with the
actions "Share something else" and "Stop sharing". "Share something else" SHALL open the browser's picker again and
replace the shared surface without leaving the presenting state; cancelling the picker SHALL keep the current share.
The banner SHALL disappear when the share ends, however it ends.

#### Scenario: Banner while presenting
- **WHEN** Ana shares her screen
- **THEN** her room shows "You are presenting." with the actions "Share something else" and "Stop sharing"
- **AND** Bruno's room shows no such banner

#### Scenario: Share something else
- **GIVEN** Ana shares a window
- **WHEN** she presses "Share something else" and picks another window
- **THEN** everyone's stage shows the new window, the banner stays and her share button still reads "Stop sharing"

#### Scenario: Picker cancelled
- **GIVEN** Ana shares a window
- **WHEN** she presses "Share something else" and cancels the browser's picker
- **THEN** the first window is still shared

#### Scenario: Share ended from the browser
- **GIVEN** Ana shares her screen
- **WHEN** she stops the share from the browser's own control
- **THEN** the banner disappears and her share button reads "Share screen"
