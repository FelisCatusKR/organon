# Tasks

## 1. Declaration and checks

- [ ] 1.1 Add `contrib/container-interface.json` and `scripts/check-deploy-interface.sh` (mise task `check:deploy-interface`) and verify deployment: Reference definitions follow the interface (passes on the current files; fails for a wrong health check, an undeclared variable, and `/data` mounted into the API)
- [ ] 1.2 Make the Podman e2e take the engine and API commands and the engine health check from the declaration, and verify the targeted e2e tests pass under rootless Podman
- [ ] 1.3 Add the CI steps (interface check; breaking marker on pull requests that change an existing declaration) and verify with actionlint and the title regex against `feat!: x`, `feat(deploy)!: x`, `feat(deploy): x`, `docs: x`

## 2. Initialization

- [ ] 2.1 Verify deployment: No implicit initialization with ERT (without `organon.json` every method is `unavailable` and the data directory is unchanged)

## 3. Documentation

- [ ] 3.1 Add "Custom deployments" to `docs/deployment.md` and the interface rule to `CONTRIBUTING.md`; create the `breaking` label

## 4. Integration

- [ ] 4.1 Run `openspec validate --all --strict`, ERT and shellcheck, then archive the change as the last commit of the pull request and verify `mise run spec:check-archived` passes
