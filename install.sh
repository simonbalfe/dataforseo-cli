#!/bin/sh
set -eu

repo="${DFS_REPO:-simonbalfe/dataforseo-cli}"
version="${DFS_VERSION:-latest}"
install_dir="${DFS_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Darwin) os="Darwin" ;;
  Linux) os="Linux" ;;
  *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="x86_64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="dfs_${os}_${arch}.tar.gz"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  release_args="--repo $repo --pattern $asset --dir $tmp_dir"
  if [ "$version" = "latest" ]; then
    gh release download $release_args
  else
    gh release download "$version" $release_args
  fi
else
  if [ "$version" = "latest" ]; then
    url="https://github.com/$repo/releases/latest/download/$asset"
  else
    url="https://github.com/$repo/releases/download/$version/$asset"
  fi
  curl -fL "$url" -o "$tmp_dir/$asset"
fi

mkdir -p "$install_dir"
tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
install -m 0755 "$tmp_dir/dfs" "$install_dir/dfs"

echo "Installed dfs to $install_dir/dfs"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to PATH to run dfs from any directory." ;;
esac
