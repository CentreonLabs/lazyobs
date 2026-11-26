#!/bin/sh
# Install or update lazyobs from its GitHub releases (macOS and Linux).
#
#   curl -fsSL https://raw.githubusercontent.com/CentreonLabs/lazyobs/main/install.sh | sh
#
# Run it again to update. Environment variables:
#   LAZYOBS_VERSION      version to install, e.g. v0.1.0 (default: the latest release)
#   LAZYOBS_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
#
# The archive is checked against the release's SHA-256 checksums before
# anything is installed. POSIX sh on purpose: it is piped to sh.
set -eu

REPO="CentreonLabs/lazyobs"
BASE_URL="${LAZYOBS_BASE_URL:-https://github.com/${REPO}/releases}" # overridable for tests
INSTALL_DIR="${LAZYOBS_INSTALL_DIR:-${HOME}/.local/bin}"

say() { printf 'lazyobs: %s\n' "$*"; }

fail() {
  printf 'lazyobs: error: %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    *) fail "unsupported system $(uname -s): download a release from https://github.com/${REPO}/releases" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    arm64 | aarch64) echo arm64 ;;
    *) fail "unsupported architecture $(uname -m)" ;;
  esac
}

# The latest release is where /releases/latest redirects to: no API call,
# hence no API rate limit.
latest_version() {
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "${BASE_URL}/latest")" ||
    fail "cannot reach ${BASE_URL}"
  latest="${latest_url##*/}"
  case "${latest}" in
    v[0-9]*) echo "${latest}" ;;
    *) fail "no release published yet" ;;
  esac
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail "sha256sum or shasum is required to check the download"
  fi
}

main() {
  need curl
  need tar
  need uname
  os="$(detect_os)"
  arch="$(detect_arch)"

  version="${LAZYOBS_VERSION:-}"
  if [ -z "${version}" ]; then
    version="$(latest_version)"
  fi
  case "${version}" in
    v*) ;;
    *) version="v${version}" ;;
  esac

  current=""
  if [ -x "${INSTALL_DIR}/lazyobs" ]; then
    current="$("${INSTALL_DIR}/lazyobs" --version 2>/dev/null | awk '{print $2}')" || current=""
  fi
  if [ "${current}" = "${version}" ]; then
    say "${version} is already installed in ${INSTALL_DIR}"
    exit 0
  fi

  name="lazyobs_${version#v}_${os}_${arch}"
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' EXIT

  say "downloading ${name}"
  curl -fsSL -o "${tmp}/${name}.tar.gz" "${BASE_URL}/download/${version}/${name}.tar.gz" ||
    fail "cannot download ${version} for ${os}/${arch}"
  curl -fsSL -o "${tmp}/checksums.txt" "${BASE_URL}/download/${version}/checksums.txt" ||
    fail "cannot download the checksums of ${version}"

  expected="$(awk -v f="${name}.tar.gz" '$2 == f || $2 == "*" f {print $1}' "${tmp}/checksums.txt")"
  [ -n "${expected}" ] || fail "${name}.tar.gz is not listed in checksums.txt"
  actual="$(sha256 "${tmp}/${name}.tar.gz")"
  [ "${actual}" = "${expected}" ] ||
    fail "checksum mismatch for ${name}.tar.gz: the download is corrupted or was tampered with"

  tar -xzf "${tmp}/${name}.tar.gz" -C "${tmp}"
  install -d "${INSTALL_DIR}"
  # Replace the binary atomically: a running lazyobs keeps its old file.
  install -m 755 "${tmp}/${name}/lazyobs" "${INSTALL_DIR}/.lazyobs.new"
  mv -f "${INSTALL_DIR}/.lazyobs.new" "${INSTALL_DIR}/lazyobs"

  if [ -n "${current}" ]; then
    say "updated ${current} → ${version} in ${INSTALL_DIR}"
  else
    say "installed ${version} in ${INSTALL_DIR}"
  fi
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *) say "add ${INSTALL_DIR} to your PATH, e.g. echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.profile" ;;
  esac
}

main
