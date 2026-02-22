# Releasing

This project uses GitHub Actions + GoReleaser for tagged releases.

Canonical release workflow: create and push a `v*` tag. CI publishes the release.

## Prerequisites

- You have push access to `jakebf/planc`.
- CI is green on the commit you want to release.
- `CHANGELOG.md` is updated (newest release at the top).

## Standard Release (CI)

1. Ensure local branch is up to date.

```bash
git checkout master
git pull --ff-only origin master
```

2. Update `CHANGELOG.md` for the version you are releasing.

3. Commit release prep changes.

```bash
git add CHANGELOG.md
git commit -m "chore: release v0.1.1 notes"
git push origin master
```

4. Create and push the release tag.

```bash
git tag -a v0.1.1 -m "v0.1.1"
git push origin v0.1.1
```

5. Watch the workflow:

- GitHub Actions -> `Release` workflow (`.github/workflows/release.yml`)
- Trigger: tag push `v*`
- Publisher: GoReleaser (`goreleaser release --clean`)

## Verify Release

```bash
gh release view v0.1.1 --repo jakebf/planc
```

Confirm:
- Archives for linux/darwin/windows
- checksums file
- `CHANGELOG.md` attached
- release notes body includes changelog content

## If a Tag Needs to Be Recut

Use this only when you intentionally want to replace an existing release/tag.

1. Make and push the fix commit.
2. Move the tag locally.

```bash
git tag -d v0.1.1
git tag -a v0.1.1 -m "v0.1.1"
```

3. Force-push the updated branch and tag.

```bash
git push --force-with-lease origin master
git push --force origin v0.1.1
```

4. If the existing GitHub release has old assets, delete it and let CI recreate, or recreate manually.

```bash
gh release delete v0.1.1 --repo jakebf/planc --yes
```

## Manual Fallback (Local GoReleaser)

Use only when CI is unavailable.

```bash
export GITHUB_TOKEN="$(gh auth token)"
goreleaser release --clean
```

If uploads fail with `422 ... already_exists`, the release already has assets for that tag. Delete and retry, or use a new tag.
