# CHANGELOG

## [v0.1.18] - 2026-07-28
### Added
- docs(readme): update demo video and improve document readability

## [v0.1.17] - 2026-07-28
### Fixed
- fix(install): resolve AIX compatibility issues and update Makefile targets
- fix(parser): always include TestFragmentController regardless of enabled state

## [v0.1.16] - 2026-07-27
### Added
- ci: enforce golangci-lint in github actions
- feat(core): implement advanced JMeter Thread Group configurations and HTTP options

## [v0.1.15] - 2026-07-24
### Added
- feat(engine): refine loop handling, flow control actions, and parser bounds
- feat(engine): implement new samplers and dynamic JTL configuration
### Fixed
- fix(engine): resolve jmx parsing bugs and evaluator concurrency issues
- fix(engine): resolve loop jump logic and UUID generation fallback

## [v0.1.14] - 2026-07-23
### Added
- feat: implement new JMeter functions and improve extractor default handling
- test(jmx): standardize file names and scale up load configurations

## [v0.1.13] - 2026-07-22
### Added
- feat(engine): implement Throughput Controller support
- feat(engine): implement closed-model concurrency and fix result merging
### Fixed
- fix(engine): use precise pacer calculation in Arrivals Thread Group

## [v0.1.12] - 2026-07-21
### Added
- feat(cli): Add --version flag to print application version
- feat(build): integrate GoReleaser and add Homebrew installation guide
### Fixed
- fix(build): bump goreleaser action version to v2 to match config
- fix(build): resolve GoReleaser build errors for AIX and checkout depth

## [v0.1.11] - 2026-07-20
### Added
- feat(ci): automate docker image publishing to GHCR
- build(docker): bump golang builder image to 1.25.12
- build(docker): inject version information during image build
- feat(security): upgrade toolchain and enhance security configurations
- feat(build): add Docker support and quick install scripts
### Changed
- perf(engine): optimize evaluator execution and runner merge performance
### Fixed
- fix(ci): inject correct release version during build
- fix(install): correct architecture mapping and syntax error

## [v0.1.10] - 2026-07-16
### Added
- ci: add GitHub Actions workflow and update READMEs
### Fixed
- fix: parse TestPlan name from testname attribute
- fix: resolve failing tests in CompareAssertion and OnceOnlyController
- fix: resolve nil pointer dereference in TestForEach

## [v0.1.1] - 2026-07-15
### Changed
- refactor: improve evaluator registry, extractor logic, and fix minor bugs
### Fixed
- fix(engine): resolve XPathExtractor bug and optimize evaluator

## [v0.1.0] - 2026-07-14
### Added
- feat(engine): implement WebSocket protocol load testing support
- feat(listener): implement View Results in Table component, Simple Data Writer component, Save Responses to a file and engine hit limit, Response Time Graph, Mailer Visualizer with SMTP support, Graph Results, Generate Summary Results parsing, Comparison Assertion Visualizer
- feat(engine): implement XPath Extractor, Result Status Action Handler, Debug PostProcessor and fix variable initialization, Boundary Extractor, JSON JMESPath Extractor, CSS Selector Extractor, Sample Timeout pre-processor, RegEx User Parameters pre-processor, HTMLLinkParser and URLRewritingModifier
- feat(preprocessor): implement HTML Link Parser parsing
- feat(assertion): implement XML Assertion parsing and evaluation, SMIME Assertion parsing, MD5Hex Assertion parsing and evaluation, Duration Assertion parsing, XPath and Compare Assertion parsing and evaluation, Size Assertion parsing and evaluation
- feat(build): inject git tag version via ldflags
### Fixed
- fix(parser): resolve ThreadGroup scope bug and staticcheck lints

## [v0.0.3] - 2026-07-13
### Added
- feat(controller): implement JMeter Module and Switch Controllers, Simple Controller, Runtime Controller, Recording Controller, Random Order Controller, Random Controller, Interleave and Once Only Controllers, Include Controller, ForEach Controller, Critical Section Controller, While Controller, Loop Controller, Transaction Controller

## [v0.0.2] - 2026-07-10
### Added
- feat(controller): implement If Controller with native expression evaluation
- feat: implement setUp and tearDown Thread Group execution lifecycle
- feat: implement Free-Form Arrivals Thread Group
- feat: implement Arrivals and Ultimate Thread Groups with robust multi-step reporting

## [v0.0.1] - 2026-07-10
### Changed
- perf: Enhance load pacing precision and worker capacity

## [v0.0.0-alpha] - 2026-06-30
### Added
- feat: support advanced JMeter components, stateful scenarios, and extensive test suites
- feat: Add support for JMeter's Timer elements, Config Elements, Ultimate Thread Group and extensive functions, bzm - Concurrency Thread Group
- feat: Add stateful variable chaining and Open Model Thread Group support
- feat: Embed Vegeta engine to remove external dependency
- feat: Add weighted multi-sampler support
- feat: Add real-time performance dashboard for vegeta attacks
- feat: Implement JMeter SteppingThreadGroup and add -force-cli option
- feat: Add report-only mode for generating HTML reports
- feat: Enhance JMX parsing, CLI, and build process
### Changed
- refactor: Implement thread-safe metrics in dashboard
- refactor: Replace if-else if chains with switch statements
- build: setup multi-platform cross-compilation and automated GitHub releases
