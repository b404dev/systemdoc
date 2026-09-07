# Contributing to Systemdoc

Start with [development](docs/development.md), [architecture](SPEC.md), and [the visual system](VISUAL-DESIGN.md).

For bugs, include `systemdoc --version`, Linux distribution/architecture, terminal, relevant backend version, reproduction steps, and sanitized output. Do not post credentials, private keys, environment values, or sensitive workload configuration. Follow [SECURITY.md](SECURITY.md) for security reports.

For a change:

1. Create a branch with a focused problem and solution.
2. Preserve system/user scope, Docker endpoint, selected workload identity, and cancellation behaviour.
3. Test meaningful behaviour with fixtures or fake commands. Live lifecycle tests belong on disposable workloads.
4. Run `make check`, `make test`, and `make cross`.
5. Update the relevant user guide, screenshots for visible changes, and [changelog](CHANGELOG.md).
6. Describe the before/after behaviour, validation, and remaining limits in the pull request.

Keep backend calls off the UI thread. Clean and escape external output. Do not add a second terminal event loop, unbounded buffers, or speculative dependencies. Preserve native authorization and explicit-target action review.

The repository license must be selected before public distribution; do not imply a license grant while release preparation is incomplete.
