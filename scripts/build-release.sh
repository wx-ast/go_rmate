#!/usr/bin/env bash
set -euo pipefail

release_tag="${1:-}"
if [[ ! "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
  echo 'Usage: bash scripts/build-release.sh v1.2.3 [output-directory]' >&2
  exit 2
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd -- "$script_dir/.." && pwd)"
mkdir -p -- "${2:-dist}"
output_dir="$(cd -- "${2:-dist}" && pwd)"
build_dir="$(mktemp -d)"
trap 'rm -rf -- "$build_dir"' EXIT

release_version="${release_tag#v}"
archives=()
cd -- "$repo_dir"

for target_os in linux darwin; do
  for target_arch in amd64 arm64; do
    package_dir="$build_dir/${target_os}_${target_arch}"
    mkdir -p -- "$package_dir"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
      go build -trimpath -ldflags "-s -w -X main.version=$release_version" \
      -o "$package_dir/rmate" .
    cp -- LICENSE README.md "$package_dir/"
    archive="rmate_${release_tag}_${target_os}_${target_arch}.tar.gz"
    tar -czf "$output_dir/$archive" -C "$package_dir" rmate LICENSE README.md
    archives+=("$archive")
  done
done

cd -- "$output_dir"
shasum -a 256 "${archives[@]}" > SHA256SUMS
echo "Release assets: $output_dir"
