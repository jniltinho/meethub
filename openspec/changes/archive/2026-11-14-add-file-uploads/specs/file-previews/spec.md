# Spec Delta

## Purpose

Lets images shared in the chat be seen without downloading them: the server derives a small thumbnail and a larger
preview from each uploaded image, safe to show inline.

## ADDED Requirements

### Requirement: Derived images
For an uploaded PNG, JPEG or GIF of at most 24 megapixels the server SHALL generate a thumbnail (at most 240 px
high) and a preview (at most 1920 px wide), both re-encoded without the original's metadata, and report
`has_preview_image`, `width` and `height` in the file info. Other files and larger images SHALL have none.

#### Scenario: Photo
- **WHEN** a member uploads a 4000 × 3000 JPEG with location metadata
- **THEN** the file info has `has_preview_image: true`, width 4000 and height 3000, and the thumbnail and preview
  carry no location metadata

#### Scenario: Not an image
- **WHEN** a member uploads a PDF
- **THEN** `has_preview_image` is false and the thumbnail and preview routes answer `404`

### Requirement: Serving derived images
`GET /api/v1/files/{id}/thumbnail` and `GET /api/v1/files/{id}/preview` SHALL return the derived images under the
same access rule as the file, as images the browser may show inline, with `nosniff` and a private cache lifetime.

#### Scenario: Inline thumbnail
- **WHEN** a member's browser loads the thumbnail of an image of their channel
- **THEN** it is displayed, and a caller outside the channel gets `404`

### Requirement: Hostile images
An image whose header declares more than 24 megapixels, or that cannot be decoded, SHALL be stored as an ordinary
file without derived images, and its upload SHALL NOT fail or decode the image in full.

#### Scenario: Decompression bomb
- **WHEN** a member uploads a small PNG declaring 30000 × 30000 pixels
- **THEN** the upload succeeds with `has_preview_image: false`
