# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1] - 2026-11-24

### Changed

- The board no longer assumes limits the libraries dropped. An admin may keep any number of active invites, not 30: every active invite is announced and kolo-swarm no longer caps its topics, so the app leaves `MaxTopics` unset. `-bootstrap`, `-relay` and `-relay-file` accept any number of entries, not at most 16. An invite still takes 1 to 100 people, the app's own bound. The error `too-many-invites` is gone.
- Requires kolo-braid, kolo-dht, kolo-invite, kolo-relay, kolo-secure, kolo-swarm, kolo-sync and kolo-transport v0.1.1.

## [0.1.0] - 2026-11-20

### Added

- First release of kolo-board-wails3-example.
