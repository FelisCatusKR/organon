# Spec Delta

## ADDED Requirements

### Requirement: Published images
Every commit on `main` whose CI run succeeded, including the unit and e2e jobs, SHALL be published to
`ghcr.io/feliscatuskr/organon` as one multi-arch image (linux/amd64 and linux/arm64). It SHALL be tagged
`sha-<first 7 hex digits of the commit>` and `main`. The image SHALL carry OCI labels for its source repository,
revision and license. Images SHALL NOT be published from pull requests or from commits whose CI failed.

#### Scenario: Commit on main
- **WHEN** a commit is pushed to `main` and all CI jobs succeed
- **THEN** `ghcr.io/feliscatuskr/organon:sha-<7>` and `:main` resolve to the same manifest list, with entries for amd64 and arm64
- **AND** the image label `org.opencontainers.image.revision` equals the full commit SHA and `organon version` prints `main-<7>`

#### Scenario: Pull request
- **WHEN** CI runs for a pull request
- **THEN** no image is pushed

#### Scenario: Failed CI on main
- **WHEN** a unit or e2e job fails for a commit on `main`
- **THEN** no image is pushed for that commit and `main` keeps pointing at the previous image

#### Scenario: Documentation-only commit
- **WHEN** a commit on `main` changes only documentation or specs, so the unit and e2e jobs are skipped
- **THEN** no new image is pushed, because the image content would be unchanged
