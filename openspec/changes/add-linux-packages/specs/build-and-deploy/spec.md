## MODIFIED Requirements

### Requirement: Only linux/amd64 is produced
The build SHALL target linux/amd64 only; no other operating system or architecture is built, packaged or released.

#### Scenario: Single artifact
- **WHEN** `make build` finishes in an empty `dist/`
- **THEN** `dist/` contains only `meethub`

#### Scenario: Packages beside the binary
- **WHEN** `make package` finishes
- **THEN** `dist/packages/` holds the `.deb`, the `.rpm` and the `.tar.gz`, each for linux/amd64 only
