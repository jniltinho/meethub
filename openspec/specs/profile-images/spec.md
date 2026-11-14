# profile-images Specification

## Purpose
Gives every user a profile picture shown beside their name in the chat, with rules that keep uploads safe to store and
serve and visible only to the people who may see that user.

## Requirements

### Requirement: Uploading a picture
`POST /api/v1/users/me/image` (multipart field `image`) SHALL accept a PNG, JPEG or GIF of at most 5 MiB and
4096 × 4096 pixels and store it as a square 256 px PNG, cropped around its center, with no metadata of the original
file. A new upload SHALL replace the previous picture.

#### Scenario: Photo with location data
- **WHEN** Ana uploads a 3000 × 2000 JPEG that carries GPS metadata
- **THEN** her picture is served as a 256 × 256 PNG without that metadata

#### Scenario: Replace
- **GIVEN** Ana has a picture
- **WHEN** she uploads another
- **THEN** everyone who may see her gets the new one

### Requirement: Rejected uploads
A file that is not a decodable PNG, JPEG or GIF, whatever its name or declared type, SHALL answer `400 invalid_image`;
one above 5 MiB SHALL answer `413`, and one above 4096 pixels on a side `400 invalid_image`, without being decoded in
full.

#### Scenario: Script renamed to PNG
- **WHEN** a user uploads an HTML file named `photo.png`
- **THEN** the answer is `400` with code `invalid_image` and the previous picture stays

#### Scenario: Decompression bomb
- **WHEN** a user uploads a small PNG that declares 30000 × 30000 pixels
- **THEN** the answer is `400 invalid_image`

### Requirement: Serving pictures
`GET /api/v1/users/{id}/image` SHALL return the picture as `image/png` with `X-Content-Type-Options: nosniff` and a
private cache lifetime to callers who may see that user under the user directory's rules; it SHALL answer `404` for a
user without a picture and for a user the caller may not see, alike.

#### Scenario: Guest and a stranger
- **GIVEN** guest Diego shares no channel or conversation with Gabriela
- **WHEN** Diego requests Gabriela's picture
- **THEN** the answer is `404`

#### Scenario: Logged out
- **WHEN** a picture is requested without a session
- **THEN** the answer is `401`

### Requirement: Removing a picture
`DELETE /api/v1/users/me/image` SHALL remove the caller's picture, and `DELETE /api/v1/admin/users/{id}/image` SHALL
let holders of `manage_users` remove another user's, recorded in the audit log with the actor and the target.

#### Scenario: Own picture
- **WHEN** Ana removes her picture
- **THEN** her initials show again everywhere

#### Scenario: Moderation
- **WHEN** an administrator removes a member's picture
- **THEN** the picture is gone and the audit log has an event naming the administrator and the member

### Requirement: Picture changes reach the others
Every upload and removal SHALL change the user's `last_picture_update` and send `user_updated` with their public
profile to the users who may see them, so open browsers replace the picture without reloading.

#### Scenario: Live change
- **GIVEN** Bruno has a conversation with Ana open
- **WHEN** Ana uploads a picture
- **THEN** Bruno sees it beside her messages within a second
