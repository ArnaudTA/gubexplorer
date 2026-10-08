# Changelog

## [1.2.2](https://github.com/ArnaudTA/gubexplorer/compare/v1.2.1...v1.2.2) (2026-10-08)


### Bug Fixes

* enhance namespace input behavior to refresh options on click ([12f928b](https://github.com/ArnaudTA/gubexplorer/commit/12f928b2fb9c58a7b54ef8405047b1427e74f95b))
* remove unnecessary condition for NAMESPACES_CONFIGMAP in deployment.yaml ([1974e26](https://github.com/ArnaudTA/gubexplorer/commit/1974e267352d33777766967aee0b325d37f2401c))

## [1.2.1](https://github.com/ArnaudTA/gubexplorer/compare/v1.2.0...v1.2.1) (2026-10-08)


### Bug Fixes

* correct resource list in RBAC configuration for pods ([8854948](https://github.com/ArnaudTA/gubexplorer/commit/88549483e2fb19396c06e76c18bd166b6c1d616f))

## [1.2.0](https://github.com/ArnaudTA/gubexplorer/compare/v1.1.2...v1.2.0) (2026-10-08)


### Features

* implement NamespaceStore for probing accessible namespaces and update deployment configuration ([2fef600](https://github.com/ArnaudTA/gubexplorer/commit/2fef600e884ab7d757248b15869693a039c1d666))

## [1.1.2](https://github.com/ArnaudTA/gubexplorer/compare/v1.1.1...v1.1.2) (2026-10-07)


### Bug Fixes

* update RBAC configuration to support cluster and namespace-scoped roles ([8df9512](https://github.com/ArnaudTA/gubexplorer/commit/8df9512c495f71bda14c38e35b6d0234a33f6b04))
* update rbacHintYAML function to remove target namespace parameter and add missing resource ([d7cc496](https://github.com/ArnaudTA/gubexplorer/commit/d7cc496971d171acdb6efae707dd8f0d816faf68))

## [1.1.1](https://github.com/ArnaudTA/gubexplorer/compare/v1.1.0...v1.1.1) (2026-10-06)


### Bug Fixes

* add branches-ignore for release-please branches in CI workflow ([fbdfc89](https://github.com/ArnaudTA/gubexplorer/commit/fbdfc89202a1bd94b3901406706d464f7e8061bc))
* add health check endpoint and update probe paths ([459f94d](https://github.com/ArnaudTA/gubexplorer/commit/459f94d7e83215099c9b20ef072ec87e43a6125c))

## [1.1.0](https://github.com/ArnaudTA/gubexplorer/compare/v1.0.0...v1.1.0) (2026-10-06)


### Features

* add RBAC hints and ServiceAccount support; introduce extraResources configuration ([7243e2d](https://github.com/ArnaudTA/gubexplorer/commit/7243e2d45c8f4d414e7b6a313e091332e5f3ee31))
* implement WebSocket terminal execution and file copy functional… ([3709c3e](https://github.com/ArnaudTA/gubexplorer/commit/3709c3e7f82404e6bfb919f6bebb8ba162ada081))
* implement WebSocket terminal execution and file copy functionality for pods; update dependencies and frontend UI ([ff9ee23](https://github.com/ArnaudTA/gubexplorer/commit/ff9ee23908ece3ebb3f6077e5739e8c7965ec6c9))
