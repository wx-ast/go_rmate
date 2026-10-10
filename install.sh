#!/usr/bin/env bash
set -euo pipefail

# Install a release binary; no Go compiler is needed.
repository='wx-ast/go_rmate'
release_base="https://github.com/$repository/releases"
install_dir="${RMATE_INSTALL_DIR:-/usr/local/bin}"
release_tag="${RMATE_VERSION:-}"

die() {
  printf 'rmate: %s\n' "$*" >&2
  exit 1
}

for tool in curl tar install uname mktemp awk; do
  command -v "$tool" >/dev/null 2>&1 || die "Required command not found: $tool"
done
if command -v sha256sum >/dev/null 2>&1; then
  checksum_command=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  checksum_command=(shasum -a 256)
else
  die 'Install sha256sum or shasum to verify the download.'
fi

case "$(uname -s)" in
  Linux) target_os=linux ;;
  Darwin) target_os=darwin ;;
  *) die 'Supported operating systems: Linux and macOS.' ;;
esac
case "$(uname -m)" in
  x86_64|amd64) target_arch=amd64 ;;
  aarch64|arm64) target_arch=arm64 ;;
  *) die 'Supported architectures: amd64 and arm64.' ;;
esac

curl_options=(-fsSL --proto '=https' --proto-redir '=https'
  --connect-timeout 10 --max-time 120 --retry 3)

if [[ -z "$release_tag" ]]; then
  printf 'Finding the latest rmate release...\n'
  if ! latest_url="$(curl "${curl_options[@]}" --head --output /dev/null \
    --write-out '%{url_effective}' "$release_base/latest")"; then
    die "Could not find the latest release. Check $release_base."
  fi
  [[ "$latest_url" == "$release_base/tag/"* ]] || die "Unexpected release URL: $latest_url"
  release_tag="${latest_url#"$release_base/tag/"}"
fi
[[ "$release_tag" == v* ]] || release_tag="v$release_tag"
[[ "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] \
  || die "Invalid release version: $release_tag"
[[ "$install_dir" == /* ]] || die 'RMATE_INSTALL_DIR must be an absolute path.'

archive="rmate_${release_tag}_${target_os}_${target_arch}.tar.gz"
download_base="$release_base/download/$release_tag"
temp_dir="$(mktemp -d)"
trap 'rm -rf -- "$temp_dir"' EXIT

printf 'Downloading rmate %s (%s/%s)...\n' "$release_tag" "$target_os" "$target_arch"
curl "${curl_options[@]}" --output "$temp_dir/$archive" "$download_base/$archive" \
  || die "Could not download $archive. Make sure the release build has finished."
curl "${curl_options[@]}" --output "$temp_dir/SHA256SUMS" "$download_base/SHA256SUMS" \
  || die 'Could not download SHA256SUMS.'

expected_checksum="$(awk -v name="$archive" '$2 == name { print $1; count++ } END { if (count != 1) exit 1 }' \
  "$temp_dir/SHA256SUMS")" || die "Missing or duplicate checksum for $archive."
[[ "$expected_checksum" =~ ^[0-9a-fA-F]{64}$ ]] || die 'Invalid SHA256 checksum.'
actual_checksum="$("${checksum_command[@]}" "$temp_dir/$archive" | awk '{ print $1 }')"
[[ "$actual_checksum" == "$expected_checksum" ]] || die 'SHA256 verification failed; nothing was installed.'

# Extract only the binary to stdout; archive paths cannot write outside temp_dir.
tar -xzOf "$temp_dir/$archive" rmate > "$temp_dir/rmate" \
  || die 'Could not extract the rmate binary.'
[[ -s "$temp_dir/rmate" ]] || die 'The release contains an empty binary.'
chmod 0755 "$temp_dir/rmate"
binary_version="$("$temp_dir/rmate" --version)" || die 'The downloaded binary cannot run on this machine.'
[[ "$binary_version" == "rmate-go ${release_tag#v}" ]] || die "Unexpected binary version: $binary_version"

use_sudo=false
if [[ ! -d "$install_dir" ]] && ! mkdir -p -- "$install_dir" 2>/dev/null; then
  command -v sudo >/dev/null 2>&1 \
    || die 'Cannot create the install directory. Set RMATE_INSTALL_DIR to a writable directory.'
  use_sudo=true
  sudo mkdir -p -- "$install_dir"
fi
if [[ ! -w "$install_dir" ]]; then
  command -v sudo >/dev/null 2>&1 \
    || die 'Cannot write to the install directory. Set RMATE_INSTALL_DIR to a writable directory.'
  use_sudo=true
fi
if "$use_sudo"; then
  sudo install -m 0755 "$temp_dir/rmate" "$install_dir/rmate"
else
  install -m 0755 "$temp_dir/rmate" "$install_dir/rmate"
fi

printf 'Installed %s to %s/rmate\n' "$binary_version" "$install_dir"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'Add this directory to PATH: export PATH="%s:$PATH"\n' "$install_dir" ;;
esac
