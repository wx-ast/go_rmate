#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd -- "$script_dir/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf -- "$test_dir"' EXIT
mkdir -p "$test_dir/bin" "$test_dir/assets" "$test_dir/package"

cat > "$test_dir/package/rmate" <<'BINARY'
#!/usr/bin/env bash
printf 'rmate-go 1.2.3\n'
BINARY
chmod 0755 "$test_dir/package/rmate"

for target_os in linux darwin; do
  for target_arch in amd64 arm64; do
    tar -czf "$test_dir/assets/rmate_v1.2.3_${target_os}_${target_arch}.tar.gz" \
      -C "$test_dir/package" rmate
  done
done
(cd "$test_dir/assets" && shasum -a 256 ./*.tar.gz | sed 's@  \./@  @' > SHA256SUMS)

cat > "$test_dir/bin/uname" <<'UNAME'
#!/usr/bin/env bash
case "$1" in
  -s) printf '%s\n' "$RMATE_TEST_OS" ;;
  -m) printf '%s\n' "$RMATE_TEST_ARCH" ;;
  *) exit 1 ;;
esac
UNAME

cat > "$test_dir/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
[[ "${RMATE_TEST_DOWNLOAD_FAIL:-0}" != 1 ]] || exit 22
url="${!#}"
if [[ "$url" == */releases/latest ]]; then
  printf 'https://github.com/wx-ast/go_rmate/releases/tag/v1.2.3'
  exit 0
fi
output=''
while [[ $# -gt 0 ]]; do
  if [[ "$1" == --output ]]; then output="$2"; shift; fi
  shift
done
cp "$RMATE_TEST_ASSETS/${url##*/}" "$output"
CURL
chmod 0755 "$test_dir/bin/uname" "$test_dir/bin/curl"

run_installer() {
  PATH="$test_dir/bin:$PATH" RMATE_TEST_ASSETS="$test_dir/assets" \
    RMATE_INSTALL_DIR="$test_dir/install directory" /bin/bash "$repo_dir/install.sh" \
    > "$test_dir/log" 2>&1
}

export RMATE_TEST_OS RMATE_TEST_ARCH
for RMATE_TEST_OS in Linux Darwin; do
  for RMATE_TEST_ARCH in x86_64 aarch64; do
    run_installer
    [[ "$("$test_dir/install directory/rmate" --version)" == 'rmate-go 1.2.3' ]]
    [[ -x "$test_dir/install directory/rmate" ]]
  done
done

# Explicit versions bypass discovery and accept the optional v prefix.
RMATE_VERSION=1.2.3 run_installer

# Failed verification must leave an already installed program untouched.
printf 'keep existing binary\n' > "$test_dir/install directory/rmate"
printf 'corrupted\n' >> "$test_dir/assets/rmate_v1.2.3_darwin_arm64.tar.gz"
RMATE_TEST_ARCH=arm64
if run_installer; then echo 'Accepted a corrupted archive' >&2; exit 1; fi
[[ "$(cat "$test_dir/install directory/rmate")" == 'keep existing binary' ]]
grep -q 'SHA256 verification failed' "$test_dir/log"

RMATE_TEST_ARCH=riscv64
if run_installer; then echo 'Accepted unsupported architecture' >&2; exit 1; fi
grep -q 'Supported architectures' "$test_dir/log"

RMATE_TEST_ARCH=x86_64
if RMATE_TEST_DOWNLOAD_FAIL=1 run_installer; then echo 'Accepted failed download' >&2; exit 1; fi
grep -q 'Could not find the latest release' "$test_dir/log"

echo 'Installer tests passed'
