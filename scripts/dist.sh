#!/usr/bin/env bash
# Build the release archives of lazyobs into ./dist: one per platform
# (tar.gz, zip for Windows) holding the binary, the license and the README,
# plus checksums.txt (SHA-256). Used by `make dist` and the release workflow.
#
# usage: scripts/dist.sh VERSION
set -euo pipefail

readonly PKG="github.com/CentreonLabs/lazyobs"
readonly PLATFORMS=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
WORK_DIR="" # removed on exit

main() {
  local version="${1:?usage: scripts/dist.sh VERSION}"
  local commit build_date ldflags
  commit="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
  build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  ldflags="-s -w -X ${PKG}/internal/version.Version=${version} -X ${PKG}/internal/version.Commit=${commit} -X ${PKG}/internal/version.Date=${build_date}"

  rm -rf dist
  install -d dist
  WORK_DIR="$(mktemp -d)"
  trap 'rm -rf "${WORK_DIR}"' EXIT

  local platform os arch name ext
  for platform in "${PLATFORMS[@]}"; do
    os="${platform%/*}"
    arch="${platform#*/}"
    name="lazyobs_${version#v}_${os}_${arch}"
    ext=""
    [[ "${os}" == windows ]] && ext=".exe"
    echo "building ${name}"
    install -d "${WORK_DIR}/${name}"
    CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" \
      go build -trimpath -ldflags "${ldflags}" -o "${WORK_DIR}/${name}/lazyobs${ext}" ./cmd/lazyobs
    install -m 644 LICENSE README.md "${WORK_DIR}/${name}/"
    if [[ "${os}" == windows ]]; then
      (cd "${WORK_DIR}" && zip -qr "${OLDPWD}/dist/${name}.zip" "${name}")
    else
      tar -C "${WORK_DIR}" -czf "dist/${name}.tar.gz" "${name}"
    fi
  done

  cd dist
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- * >checksums.txt
  else
    shasum -a 256 -- * >checksums.txt
  fi
  cat checksums.txt
}

main "$@"
