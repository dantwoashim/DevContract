# Release and Reproducibility Notes

EnvSync ships source archives directly from git, not from an arbitrary working directory.

## Source Packaging

Use:

```bash
bash ./scripts/package_source.sh dist/source "${VERSION}"
```

This script creates:

- `envsync-${VERSION}-source.tar.gz`
- `envsync-${VERSION}-source.zip`

Both archives are generated with `git archive`, which keeps the source bundle aligned to the tagged commit.

## What Is Included

The source bundle includes tracked repository content only. It excludes untracked local state such as:

- local `.env` files
- build caches
- compiled binaries
- `node_modules`
- generated extension packages

## CI Validation

CI validates the source bundle contents so release packaging does not silently drift.

Current release expectations:

- source archives are built from the tagged commit
- `scripts/check_repo_hygiene.sh` passes before packaging
- the bundle contains tracked source only
- Go tests pass before release packaging

## Reproducible Review Workflow

To review a release artifact locally:

1. check out the release tag
2. run `bash ./scripts/package_source.sh`
3. inspect the resulting tarball or zip
4. compare the archive prefix and file list against the tagged tree

This keeps source review deterministic and avoids shipping local workstation residue in public release bundles.
