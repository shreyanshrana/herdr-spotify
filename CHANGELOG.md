# Changelog

## 0.1.0

- Spotify playback status and controls for Herdr on macOS and Linux.
- Desktop-first playback on macOS, with Spotify Web API fallback.
- OAuth PKCE sign-in, cached liked-song state, and like/unlike controls.
- Shared polling cache, credential locking, and persistent API cooldowns,
  including short `Retry-After` responses.
- Safe launcher setup that requires `--force` to replace unrelated files or links.
- A copied launcher resolves the current registered installation after updates.
- Herdr-managed config and state directories, a shared location record for direct
  commands, and non-destructive migration of legacy credentials and cooldowns.
- Prefix followed by Alt+L for liking songs without the GPUI Shift+L conflict.
- Prebuilt archives for macOS and Linux on ARM64 and AMD64, with SHA-256 checksums.
