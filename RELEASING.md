# Releasing

Releases must be done locally (macOS code signing requires Keychain access).

## Prerequisites

- `brew install goreleaser`
- Apple Developer ID Application certificate installed in Keychain
- App-specific password for notarization ([generate here](https://appleid.apple.com/account/manage))
- GitHub PAT with `homebrew-tap` repo write access

## Release

```bash
# Delete old release if re-releasing same version
gh release delete vX.Y.Z --yes 2>/dev/null; git tag -d vX.Y.Z 2>/dev/null; git push origin :refs/tags/vX.Y.Z 2>/dev/null

# Tag
git tag -a vX.Y.Z -m "vX.Y.Z"

# Release (signs + notarizes macOS binaries)
export GITHUB_TOKEN=$(gh auth token) && \
  export HOMEBREW_TAP_GITHUB_TOKEN=<PAT> && \
  export APPLE_NOTARY_PASSWORD=<APP_SPECIFIC_PASSWORD> && \
  goreleaser release --clean
```
