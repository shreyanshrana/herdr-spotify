#!/bin/sh
set -eu

force=false
case "${1:-}" in
    '') ;;
    --force) force=true; shift ;;
    *) printf '%s\n' 'Usage: setup.sh [--force]' >&2; exit 1 ;;
esac
if [ "$#" -ne 0 ]; then
    printf '%s\n' 'Usage: setup.sh [--force]' >&2
    exit 1
fi

root=${HERDR_PLUGIN_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}
binary="$root/bin/herdr-spotify"
launcher="$HOME/.local/bin/herdr-spotify"

if [ ! -x "$binary" ]; then
    printf '%s\n' 'Build the plugin first with: make build' >&2
    exit 1
fi
if [ -d "$launcher" ]; then
    printf 'Cannot replace directory: %s\n' "$launcher" >&2
    exit 1
fi
if [ -e "$launcher" ] || [ -L "$launcher" ]; then
    if [ "$force" != true ] && { [ ! -L "$launcher" ] || [ "$(readlink "$launcher")" != "$binary" ]; }; then
        printf 'Refusing to replace existing launcher: %s\nReview it, then run sh "%s/scripts/setup.sh" --force to replace it.\n' "$launcher" "$root" >&2
        exit 1
    fi
fi

mkdir -p "$HOME/.local/bin"
ln -sf "$binary" "$launcher"
printf 'Installed launcher: %s\n' "$launcher"
printf 'Merge the settings from %s into your Herdr config, then choose menu → reload config.\n' "$root/examples/herdr-config.toml"
