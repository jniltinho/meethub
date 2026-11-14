# Spec Delta

## ADDED Requirements

### Requirement: File settings
The configuration SHALL have a `files` section: `driver` (`local` or `s3`), `max_size_bytes`, `max_per_post`,
`local.dir`, and `s3.endpoint`, `s3.bucket`, `s3.region` and `s3.use_ssl`; the S3 access key and secret SHALL be
accepted only from `MEETHUB_FILES_S3_ACCESS_KEY` and `MEETHUB_FILES_S3_SECRET_KEY`. Validation SHALL reject an
unknown driver, non-positive limits and an `s3` driver without endpoint, bucket or credentials.

#### Scenario: S3 without credentials
- **WHEN** the server starts with `files.driver = "s3"` and no secret in the environment
- **THEN** it exits with an error naming `files.s3` and never prints a credential

#### Scenario: Secret in the file
- **WHEN** `config.toml` contains an S3 secret key
- **THEN** startup fails saying secrets come only from the environment
