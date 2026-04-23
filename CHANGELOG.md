# Changelog

## [0.1.3](https://github.com/DND-IT/action-deployer/compare/v0.1.2...v0.1.3) (2026-04-23)


### Bug Fixes

* nil pointer panic in WriteOutputs/WriteStepSummary on deploy error; bump go 1.26.2 ([3999e18](https://github.com/DND-IT/action-deployer/commit/3999e183ce424d631177c21309389a5aa2b3d9db))

## [0.1.2](https://github.com/DND-IT/action-deployer/compare/v0.1.1...v0.1.2) (2026-04-16)


### Features

* **deployer:** include environment names in auto-deploy commit message ([ce9044e](https://github.com/DND-IT/action-deployer/commit/ce9044eb8e18e96a4354db9fb1733ef75e1ab7d1))


### Bug Fixes

* add safe.directory for Docker container compatibility ([4ee05b8](https://github.com/DND-IT/action-deployer/commit/4ee05b8c75206def4695ccc0d8e471cb32965048))
* address golangci-lint errcheck violations ([c995686](https://github.com/DND-IT/action-deployer/commit/c9956867172c051823a0b9361bc3ddf3e590bcec))
* **summary:** pass version to step summary ([c283f9c](https://github.com/DND-IT/action-deployer/commit/c283f9ced86471543304a08fe75b76568af912bd))

## [0.1.1](https://github.com/DND-IT/action-deployer/compare/v0.1.0...v0.1.1) (2026-04-15)


### Features

* initial implementation ([f619af5](https://github.com/DND-IT/action-deployer/commit/f619af591820ac13458c3bbf70d4e40f0e156fc1))
