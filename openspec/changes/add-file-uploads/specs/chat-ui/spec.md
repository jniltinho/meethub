# Spec Delta

## ADDED Requirements

### Requirement: Attaching files in the composer
The composer SHALL attach files through an attach button, by dropping them on the conversation and by pasting
images, show each with its name, size, upload progress and a way to remove it, keep a failed one with Retry, and
send the message once every upload has finished; it SHALL refuse, with a themed message, a file above the size
limit or beyond the number a message may carry.

#### Scenario: Drop a file
- **WHEN** the user drops `plan.xlsx` on the conversation and presses Enter
- **THEN** the message appears with the file and the other members see it within a second

#### Scenario: Too many files
- **WHEN** the user selects 12 files at once
- **THEN** 10 are attached and a themed message says a message carries at most 10 files

#### Scenario: Paste a screenshot
- **WHEN** the user pastes an image from the clipboard into the composer
- **THEN** it is attached with a preview before sending

### Requirement: Sending while files upload
A message submitted while its files upload SHALL leave with the text and the files it had when it was submitted,
whatever is typed or attached meanwhile; when one of those uploads fails nothing SHALL be sent and the text SHALL
return to the composer. Dragging text or links over the conversation SHALL keep its usual behavior.

#### Scenario: Typing while the upload finishes
- **GIVEN** the user pressed Enter on "Plan attached" while `plan.xlsx` was uploading
- **WHEN** they type another draft before the upload finishes
- **THEN** the message sent is "Plan attached" with the file, and the new draft stays in the composer

#### Scenario: Upload fails after Enter
- **GIVEN** the user pressed Enter while a file was uploading
- **WHEN** that upload fails
- **THEN** no message is sent, the file shows Retry and the text is back in the composer

### Requirement: Files inside messages
A message SHALL show its images as thumbnails that open enlarged (with a way to download the original, closed by
Escape, focus returning to the thumbnail) and its other files as cards with a type icon, name, size and a download
action; a tombstone SHALL show none.

#### Scenario: Image opens enlarged
- **WHEN** the user activates a thumbnail
- **THEN** the preview opens in a themed dialog and Escape closes it

#### Scenario: Download a document
- **WHEN** the user activates the download action of `plan.xlsx`
- **THEN** the browser downloads the file under that name

### Requirement: Channel files
The channel header SHALL have a Files action that lists the channel's files in the side panel, newest first, with
uploader and date, each opening its message in the channel; the channel details SHALL show the five most recent
files. The lists SHALL follow the channel: a new message's files appear and a deleted message's files leave
without reloading.

#### Scenario: Files panel
- **WHEN** the user presses Files in the header of `#projects`
- **THEN** the side panel lists the channel's files, and choosing one opens the channel at its message

### Requirement: Files page
The Files section SHALL list the files of the user's channels and conversations with name, channel, uploader, date
and size, filter them by name as the user types, load more on scroll, and offer download and "go to message" for
each.

#### Scenario: Find a file
- **WHEN** the user opens Files and types `plan`
- **THEN** `plan.xlsx` is listed with its channel, and "go to message" opens that channel at the message
