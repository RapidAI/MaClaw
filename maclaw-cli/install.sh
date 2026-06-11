#!/usr/bin/env sh
set -eu

if [ "$#" -gt 0 ]; then
  install_dir="$1"
else
  install_dir="$HOME/.local/bin"
fi
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"
out_path="$install_dir/maclaw-cli"

mkdir -p "$install_dir"
cd "$repo_root"
go build -o "$out_path" ./maclaw-cli
chmod +x "$out_path"

printf 'Installed maclaw-cli to %s\n' "$out_path"
printf 'Run: %s agent-help\n' "$out_path"
printf 'Add %s to PATH if needed.\n' "$install_dir"
