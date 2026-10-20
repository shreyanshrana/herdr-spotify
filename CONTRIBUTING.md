# Contributing

## Development

Use Go 1.24 or newer on macOS or Linux. No external Go dependencies are required.

```sh
make check
make build
```

`make check` checks formatting, runs race-enabled tests, and runs `go vet`.
Tests use simulated Spotify API and desktop backends, not a real account.
See the README's local development section for linking the plugin to Herdr.

## Release checklist

1. Update `version` in `herdr-plugin.toml`, the default `VERSION` in `Makefile`,
   and the release notes in `CHANGELOG.md`. Update versioned README examples.
2. Review `git status` and the staged diff. Do not include credentials,
   `.env` files, generated binaries, or local polling state.
3. Run `make check` and build archives for the release version:

   ```sh
   make package VERSION=0.1.0
   (cd dist && shasum -a 256 -c SHA256SUMS)
   ```

4. Smoke-test the native archive's setup using a temporary home directory.
5. Commit the reviewed changes and verify the destination repository and
   publishing account before pushing.
6. Create and push a matching `v0.1.0`-style Git tag. The release workflow
   checks the code, builds all four platform archives, verifies their checksums,
   and publishes a GitHub release with generated release notes.

Packaging rejects a version that differs from `herdr-plugin.toml`. Outputs go
to `dist/` and are ignored by Git. Archives include the changelog but no user
credentials or local polling state.

For marketplace discovery, add the `herdr-plugin` GitHub topic to the public
repository containing `herdr-plugin.toml`.
