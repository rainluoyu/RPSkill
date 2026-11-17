#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="$root/web/dist"
target_dir="$root/internal/server/ui"
if [[ ! -f "$source_dir/index.html" ]]; then
  printf 'Missing web/dist/index.html: build the Vue frontend first.\n' >&2
  exit 1
fi
if find "$source_dir" -type l -print -quit | grep -q .; then
  printf 'Refusing symlinks in web/dist.\n' >&2
  exit 1
fi
# Both paths are derived from this script, never from RP/user content.
mkdir -p "$target_dir"
find "$target_dir" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -R "$source_dir/." "$target_dir/"
printf 'Synced web/dist to internal/server/ui.\n'
