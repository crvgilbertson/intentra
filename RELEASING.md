# Release Checklist

## Prerequisites

1. **Homebrew tap** — Before the first release, create the tap repo:
   - [crvgilbertson/homebrew-intentra](https://github.com/crvgilbertson/homebrew-intentra)
   - Empty repo; GoReleaser will push the formula on release.
   - **Permissions:** `GITHUB_TOKEN` can push to same-owner repos if both are under your account. If you hit "resource not accessible by integration" when GoReleaser pushes to the tap, add a Personal Access Token with `repo` scope as `GITHUB_TOKEN` (or a custom secret) in the release workflow. See [GoReleaser: resource not accessible](https://goreleaser.com/errors/resource-not-accessible-by-integration/).

2. **Changelog** — Update `CHANGELOG.md` for the new version. Version is injected at build time via ldflags; no manual `internal/version.go` bump needed.

## Cut a Release

```bash
git tag v0.5.0
git push origin v0.5.0
```

The [release workflow](.github/workflows/release.yml) runs on tag push. GoReleaser:

- Builds binaries for macOS (arm64, amd64), Linux (amd64, arm64), Windows (amd64)
- Uploads to [GitHub Releases](https://github.com/crvgilbertson/intentra/releases)
- Generates checksums
- Publishes the Homebrew formula to the tap (if configured)

## Post-Release

1. Verify binaries uploaded to GitHub Releases
2. Verify checksums file
3. Test: `brew tap crvgilbertson/intentra && brew install intentra`
4. Publish release notes with strong changelog summary

## Pre-Releases (RCs)

Tag with `-rcN` suffix; GoReleaser treats these as pre-releases (not latest):

```bash
git tag v0.6.0-rc1
git push origin v0.6.0-rc1
```

`intentra --version` will report `v0.6.0-rc1`.

## Rollback a Bad Release

1. Delete the tag locally and remotely: `git tag -d v0.5.0` then `git push origin :refs/tags/v0.5.0`
2. In GitHub: Releases → Edit the draft release → Delete the release
3. Fix the issue, then re-tag and push

## Verify Checksum Integrity

After downloading a binary:

```bash
# Download checksums from the release
curl -sLO https://github.com/crvgilbertson/intentra/releases/download/v0.5.0/checksums.txt
# Verify (macOS/Linux)
sha256sum -c checksums.txt
# On macOS: shasum -a 256 -c checksums.txt
```

## Local Snapshot (Test Before Tagging)

```bash
goreleaser release --snapshot --clean
```

Builds artifacts locally without publishing. Verify `dist/` contents (no stray files, binary + LICENSE only).

## Future: Signed Checksums

For long-term trust, enable artifact signing in `.goreleaser.yaml`:

```yaml
signs:
  - artifacts: checksum
```

Requires GPG key setup. Not urgent for 0.5.
