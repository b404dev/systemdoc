# Releasing Systemdoc

[Documentation index](README.md)

## Public repository setup

Before the first release:

1. Confirm the public `owner/repository` and default branch. Replace `OWNER/systemdoc` in README, installation instructions, and `install.sh`; adjust raw URLs if the branch is not `main`.
2. Choose a license, add its full text as `LICENSE`, and update README/contribution notes. Public visibility alone does not grant an open-source license.
3. Enable GitHub Actions and private vulnerability reporting. Confirm workflow token permissions allow the release job to create drafts.
4. Run the checks below, commit the release content, and configure the Git remote. The documentation and installer are local preparation until pushed.

## Validate

```sh
make check
make test
make cross
make release VERSION=v0.1.0
./dist/systemdoc-linux-amd64 --version  # on an amd64 host
(cd dist && sha256sum -c SHA256SUMS)
```

Replace the example tag with the version being released. `make release` requires an explicit `v…` version and emits:

```text
dist/systemdoc-linux-amd64
dist/systemdoc-linux-arm64
dist/systemdoc-darwin-arm64
dist/SHA256SUMS
```

Run each binary on its matching architecture. Record the tested distributions, terminals, systemd/Docker versions, and manual integration results in release notes. Exercise service/container read and lifecycle workflows only against disposable workloads; check system/user scope, Docker contexts, Compose, SSH upload/cleanup, and provider adapters if advertised. Verify all themes, compact layouts, overlays, and long Docker mount paths.

Review [known limits](troubleshooting.md#current-boundaries) and [the changelog](../CHANGELOG.md). Do not turn an untested integration into a release claim.

## Create the draft

After the release commit is pushed, a `v…` tag triggers `.github/workflows/release.yml`. The workflow reruns checks, builds both static binaries with the tag embedded in `--version`, creates `SHA256SUMS`, and creates a **draft** GitHub release with generated notes. It does not automatically publish the draft.

```sh
git tag -a v0.1.0 -m 'Systemdoc v0.1.0'
git push origin v0.1.0
```

Inspect assets, checksums, versions, and notes in the draft. Publish only after review. Mark prereleases appropriately; the installer defaults to GitHub's latest published regular release. A draft or prerelease alone does not make the default one-liner work. Keep published tags/assets immutable in practice: ship corrections under a new version.

## Verify the public installer

After publishing, run the README command in a disposable Linux environment. Confirm the resolved tag, architecture, checksum, installed permissions, PATH message, and `systemdoc --version`. Test `SYSTEMDOC_VERSION` against the published tag and an update over an existing installation. Remove the release-preparation notice from README once the public path has been verified.

Offline installer tests cover these mechanics without a GitHub release, but cannot prove that an unpublished URL is reachable.

Workflow references: [GitHub Go builds](https://docs.github.com/en/actions/tutorials/build-and-test-code/go), [GitHub CLI release creation](https://cli.github.com/manual/gh_release_create), [latest-release semantics](https://docs.github.com/en/rest/releases/releases#get-the-latest-release).

## macOS assets and validation

Release builds and the workflow also include `systemdoc-darwin-arm64` in `SHA256SUMS`. macOS release readiness requires the live checks in [macOS services](macos.md), including installer and protected-service behaviour; cross-compilation alone is insufficient. No signing/notarization pipeline is configured.
