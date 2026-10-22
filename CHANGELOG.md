# Changelog

All notable changes to this project are documented in this file. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-22

### Added

- Initial source release, with a CLI and Go API for merging leads from CSV files, Google
  Sheets and Apollo webhooks, then scoring them with a configurable YAML rubric.
- Cold outreach selects one lane per person; matching non-cold lanes and export lists
  are evaluated independently. Opt-out checks and a durable call ledger prevent known
  opt-outs and repeated cold pushes.
- Apollo pushes wait for a received unsubscribe webhook unless a configured HubSpot sink
  provides its opt-out lookup. Configuring a receiver alone does not enable pushes.
- Doctor, status, ranking and score explanations, with SQLite and Google Sheets stores
  and conformance suites for plug-in stores and sinks.
- Source builds for standalone, Docker and Google Cloud use. Docker runs a locally built
  image; cloud setup requires an explicit region.
- Four offline examples, a visual demo using synthetic leads, and personal-data guidance
  covering retention, opt-outs and whole-workspace erasure.

[Unreleased]: https://github.com/tetriz-ai/leadscore/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tetriz-ai/leadscore/releases/tag/v0.1.0
