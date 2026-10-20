# herdr-spotify

Spotify now-playing text, keyboard playback controls, and a like/unlike toggle for Herdr's bottom status area. Written in Go with no external dependencies. Supports macOS and Linux, tracks and podcasts, and paused playback. On macOS the desktop app is preferred; the Web API is the fallback. API playback controls require Spotify Premium.

## Requirements

- Herdr **0.9.1 or newer**.
- macOS or Linux (ARM64 or AMD64). Windows is not supported.
- Go **1.24 or newer** and Git for installation from GitHub. Release archives include prebuilt binaries and don't require Go.
- On macOS, Spotify's desktop app for desktop playback. On Linux, or for Web API fallback, your own Spotify developer app.

## Install as a Herdr plugin

Install from GitHub:

Run the action commands with a Herdr session running. Installation and linking can happen before starting Herdr, but setup and login actions require its server.

```sh
herdr plugin install shreyanshrana/herdr-spotify
herdr plugin action invoke herdr-spotify.setup
```

Herdr builds `bin/herdr-spotify` during installation. The setup action creates `~/.local/bin/herdr-spotify` as a symlink to that binary. Repeating setup for the same binary is safe; other existing files or symlinks are left untouched. To replace one intentionally, inspect it first, then run `sh scripts/setup.sh --force` from the new plugin directory. Directories are never replaced. This stable launcher lets the status bar invoke the executable without knowing Herdr's managed checkout path. Setup prints the location of the configuration example; it doesn't change your Herdr config.

Add `~/.local/bin` to your shell's `PATH` to use `herdr-spotify` directly, or use the full path in terminal commands.

### Local development

From the source checkout:

```sh
make build
herdr plugin link "$PWD"
herdr plugin action invoke herdr-spotify.setup
```

`plugin link` does not run build commands. Run `make build` after code changes; the launcher follows the rebuilt binary.

### Prebuilt release

Download a `.tar.gz` archive for your system and `SHA256SUMS` from GitHub Releases. Archives are available for `darwin_arm64`, `darwin_amd64`, `linux_arm64`, and `linux_amd64`. Each contains a binary, manifest, setup scripts, and configuration example.

Verify the downloaded archive's SHA-256 against its line in `SHA256SUMS`, then extract and link it. For example, on Apple Silicon:

```sh
tar -xzf herdr-spotify_0.1.0_darwin_arm64.tar.gz
herdr plugin link "$PWD/herdr-spotify_0.1.0_darwin_arm64"
herdr plugin action invoke herdr-spotify.setup
```

Keep the extracted directory in a permanent location: the launcher and linked plugin reference it. Linking a prebuilt release does not require Go. If you later switch to GitHub-managed installation, first run `herdr plugin unlink herdr-spotify`.

## Sign in

On macOS, the running Spotify desktop app works without API sign-in for playback. Sign in to enable liking songs, the heart indicator, and API fallback when the app is closed or inaccessible. Linux requires API sign-in.

1. Create an app at https://developer.spotify.com/dashboard and select Web API.
2. Add this exact Redirect URI: `http://127.0.0.1:6061/callback`.
3. Copy the app's **Client ID**. No client secret is needed.
4. Run:

   ```sh
    ~/.local/bin/herdr-spotify login --client-id YOUR_CLIENT_ID
   ```

5. Approve access in the browser, then play something on Spotify.

Development-mode apps require your Spotify account to be listed in the app's User Management. For another port, use `login --port 8888` and update the dashboard Redirect URI accordingly. No Client ID or credentials are bundled with this plugin.

Alternatively, run `herdr plugin action invoke herdr-spotify.login` to open a Herdr sign-in popup that prompts for your Client ID (using the default callback port). To keep errors visible or change the port, use the terminal command instead.

## Herdr configuration

Merge [examples/herdr-config.toml](examples/herdr-config.toml) into `~/.config/herdr/config.toml`. Add the status command to an existing `tab_bar_right` list rather than replacing other entries, and add the four keybindings only if those keys are free. Don't duplicate an existing `[ui]` section.

```toml
[ui]
tab_bar_position = "bottom"
hide_tab_bar_when_single_tab = false
tab_bar_right = [
  { type = "command", command = "~/.local/bin/herdr-spotify status", interval_seconds = 5, timeout_seconds = 10 },
]

[[keys.command]]
key = "prefix+comma"
type = "plugin_action"
command = "herdr-spotify.prev"
description = "Spotify: previous track"

[[keys.command]]
key = "prefix+space"
type = "plugin_action"
command = "herdr-spotify.toggle"
description = "Spotify: play/pause"

[[keys.command]]
key = "prefix+period"
type = "plugin_action"
command = "herdr-spotify.next"
description = "Spotify: next track"

[[keys.command]]
key = "prefix+alt+l"
type = "plugin_action"
command = "herdr-spotify.like"
description = "Spotify: toggle liked song"
```

Reload from Herdr's global menu using **reload config** (refreshes the client and server). `herdr server reload-config` refreshes the server settings; the attached client's tab placement may require the menu reload or a reattach.

The display shares the bottom row with tabs and yields to them on narrow terminals:

```text
[tab 1] [tab 2]       ⏮ ,   ⏸ Space   ⏭ .   ♥ Alt+L  │  Track — Artist · 1:24 / 3:52 · 🖥
```

Run `herdr-spotify status` to check output directly. Nothing playing produces no output. If desktop playback is unavailable and you haven't signed in for API access, the bar shows a login hint. Playback position is refreshed every five seconds, not animated between polls. The source icon is `🖥` for the desktop app and `🌐` for the Web API; emoji appearance and width depend on your terminal font.

## Playback controls

Press Herdr's prefix (default `Ctrl+B`), release it, then press:

| Key | Action |
| --- | --- |
| `,` | Previous track |
| `Space` | Play/pause |
| `.` | Next track |
| `Alt+L` | Toggle current song in Liked Songs |

The like shortcut uses `Alt+L` because GPUI reserves prefix followed by `Shift+L` for Swap Pane Right. If upgrading an older configuration, replace `prefix+shift+l` with `prefix+alt+l`. These hints assume the example bindings; update your configuration consistently if you choose different shortcuts.

The bar's labels are keyboard hints, not clickable buttons: Herdr 0.9.1's status area has no click-action API. Each icon is separated from its shortcut key, and a vertical divider separates controls from playback details. Titles longer than 28 characters and artist names longer than 20 characters are shortened with an ellipsis. The middle label shows the action available (`⏸` while playing, `▶` while paused).

If you signed in before API playback controls were added, run `herdr-spotify login --client-id YOUR_CLIENT_ID` again to grant the new permission. Desktop controls need no OAuth scopes. API controls require an active Spotify device. Successful API controls invalidate the cache so the next bar refresh fetches the new state; desktop playback is read directly on each refresh.

You can also run `herdr-spotify prev`, `next`, `toggle`, `play`, `pause`, or `like` directly. Shortcut commands run detached; for a visible error message, run the command in a terminal.

### Liking songs

`herdr-spotify like` toggles the current song in Spotify's **Liked Songs**: unsaved songs are saved, and saved songs are removed. It checks Spotify's current saved state on every press, including changes made in another app. The song is identified once per invocation, so a track change during the request does not switch the target. Paused songs are supported; podcasts and local files are not.

API sign-in is required even when playback comes from the macOS desktop app. If you previously signed in, run `herdr-spotify login --client-id YOUR_CLIENT_ID` again to grant `user-library-read` and `user-library-modify`.

The bar shows `♡ Alt+L` for an unsaved song and `♥ Alt+L` for a saved song when the state is known. Saved-state lookups are cached for one minute per current song; changes made in another app appear after that refresh. A successful toggle updates the cached heart immediately and refreshes API playback on the next status poll. Missing sign-in or failed lookups omit the heart. Library requests honor the shared API rate-limit cooldown, while desktop playback remains available.

### Plugin actions

| Action | Purpose |
| --- | --- |
| `herdr-spotify.setup` | Install the stable status-bar launcher |
| `herdr-spotify.login` | Open the API sign-in popup |
| `herdr-spotify.prev` | Previous track |
| `herdr-spotify.toggle` | Play/pause |
| `herdr-spotify.next` | Next track |
| `herdr-spotify.play` | Resume playback |
| `herdr-spotify.pause` | Pause playback |
| `herdr-spotify.like` | Toggle current song in Liked Songs |

Inspect actions with `herdr plugin action list --plugin herdr-spotify`. For action diagnostics, use `herdr plugin log list --plugin herdr-spotify`.

## Storage and network

### Playback sources

On macOS, status and all playback shortcuts prefer the running Spotify desktop app through local automation (`osascript`). The bar adds `🖥` to identify this source. Desktop playback is checked on each five-second bar refresh, even after an API cooldown expires. When signed in with library permissions, the heart indicator uses cached Web API lookups. An open but idle app is authoritative and does not trigger API polling.

If Spotify is closed or its playback state cannot be read, the helper falls back to the Web API and shows `🌐`. Existing API rate-limit deadlines remain in force and are preserved across sign-ins. Linux uses the API directly. Windows is not currently supported because file locking uses Unix APIs.

Spotify must already be running; the helper does not launch it. If macOS asks to allow control of Spotify, approve it. Automation permissions can be reviewed under **System Settings → Privacy & Security → Automation**. The app supplies its current playback state; API fallback provides cross-device tracking. If a desktop control command fails after a successful state lookup, the error is reported without repeating the action through the API, avoiding a possible double toggle or skip.

### Credentials and polling

- Credentials and polling state live in `~/.config/herdr-spotify/`. Credential files are owner-readable/writable only.
- Sign-in uses Authorization Code with PKCE and scopes `user-read-currently-playing`, `user-read-playback-state`, `user-modify-playback-state`, `user-library-read`, and `user-library-modify`.
- API requests go to Spotify. The callback listens only on `127.0.0.1` during sign-in.
- On macOS, the desktop source uses local automation for playback; liked-state checks and toggles use the Spotify Web API.
- Commands share a file lock and cached result, so multiple Herdr sessions avoid duplicate refreshes and polls. Rate-limit delays and failure backoff persist between invocations.
- Under `herdr --remote`, status commands execute on the server machine, where this binary and its credentials must be installed.

## Verify

```sh
make check
```

GitHub CI checks formatting, race tests, vet, and native builds on macOS and Linux, and cross-compiles all four release targets. Tests use simulated API and desktop backends, not your Spotify account.

## Updates and removal

For GitHub-managed installs, reinstall the same repository to update:

```sh
herdr plugin install shreyanshrana/herdr-spotify
```

The launcher follows the managed checkout. For linked source checkouts, rebuild with `make build`. For prebuilt releases, unlink the old directory, link the new one, and run `sh scripts/setup.sh --force` from the new directory after verifying the existing launcher. Credentials and cooldown state remain outside the checkout.

To remove:

1. Remove the Spotify entry from `tab_bar_right` and its four `[[keys.command]]` blocks, then reload Herdr's config.
2. Run `herdr plugin uninstall herdr-spotify` for a GitHub install, or `herdr plugin unlink herdr-spotify` for a linked plugin.
3. Remove the launcher:

   ```sh
   rm ~/.local/bin/herdr-spotify
   ```

4. Optionally remove `~/.config/herdr-spotify/` and revoke API access at https://www.spotify.com/account/apps/.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and release instructions, and [CHANGELOG.md](CHANGELOG.md) for release history.

## License

MIT. See [LICENSE](LICENSE).
