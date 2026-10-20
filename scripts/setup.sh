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
dispatcher="$root/bin/herdr-spotify-launcher"
launcher="$HOME/.local/bin/herdr-spotify"

if [ ! -x "$binary" ] || [ ! -x "$dispatcher" ]; then
    printf '%s\n' 'Build the plugin first with: make build' >&2
    exit 1
fi
if [ -d "$launcher" ]; then
    printf 'Cannot replace directory: %s\n' "$launcher" >&2
    exit 1
fi
if [ -e "$launcher" ] || [ -L "$launcher" ]; then
    same=false
    if [ -L "$launcher" ]; then
        [ "$(readlink "$launcher")" != "$binary" ] || same=true
    elif [ -f "$launcher" ] && cmp -s "$dispatcher" "$launcher"; then
        same=true
    fi
    if [ "$force" != true ] && [ "$same" != true ]; then
        printf 'Refusing to replace existing launcher: %s\nReview it, then run sh "%s/scripts/setup.sh" --force to replace it.\n' "$launcher" "$root" >&2
        exit 1
    fi
fi

"$binary" --init-storage
mkdir -p "$HOME/.local/bin"
temporary=$(mktemp "$HOME/.local/bin/.herdr-spotify.XXXXXX")
trap 'rm -f "$temporary"' EXIT HUP INT TERM
cp "$dispatcher" "$temporary"
chmod 755 "$temporary"
mv -f "$temporary" "$launcher"
printf 'Installed launcher: %s\n' "$launcher"
printf 'Merge the settings from %s into your Herdr config, then choose menu → reload config.\n' "$root/examples/herdr-config.toml"
