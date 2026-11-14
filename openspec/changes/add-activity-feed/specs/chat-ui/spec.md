# Spec Delta

## ADDED Requirements

### Requirement: Activity page
The Activity page SHALL list each item with the author's picture and name, the channel, whether it is a mention or
a reply, the start of the message and its time, loading more on scroll; choosing an item SHALL open its channel at
that message (in its thread for a reply). The Activity entry of the navigation SHALL show the unseen count, and
opening the page SHALL clear it.

#### Scenario: Open a mention
- **WHEN** Ana chooses a mention from `#projects` in Activity
- **THEN** `#projects` opens at that message, highlighted

#### Scenario: Badge
- **GIVEN** Ana has two unseen items
- **WHEN** she opens Activity
- **THEN** the badge showed 2 and is gone once the page is open
