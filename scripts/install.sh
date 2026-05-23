#!/usr/bin/env bash
set -euo pipefail

REPO="${LSP_BRIDGE_REPO:-muidea/lsp-bridge}"
if [ -n "${LSP_BRIDGE_INSTALL_DIR:-}" ]; then
  INSTALL_ROOT="$LSP_BRIDGE_INSTALL_DIR"
elif [ -n "${HOME:-}" ]; then
  INSTALL_ROOT="${HOME}/.local"
else
  printf '[lsp-bridge install] error: HOME is required when LSP_BRIDGE_INSTALL_DIR is not set\n' >&2
  exit 1
fi
VERSION="${LSP_BRIDGE_VERSION:-latest}"
INSTALL_PYRIGHT="${INSTALL_PYRIGHT:-1}"
INSTALL_GOPLS="${INSTALL_GOPLS:-1}"

BIN_DIR="${INSTALL_ROOT}/bin"
NODE_PREFIX="${INSTALL_ROOT}/.deps/node"
CLEANUP_DIR=""

cleanup() {
  if [ -n "${CLEANUP_DIR:-}" ]; then
    rm -rf "$CLEANUP_DIR"
  fi
}
trap cleanup EXIT

log() {
  printf '[lsp-bridge install] %s\n' "$*"
}

die() {
  printf '[lsp-bridge install] error: %s\n' "$*" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

sudo_cmd() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif has_cmd sudo; then
    sudo "$@"
  else
    die "missing sudo; install $* manually or run as root"
  fi
}

install_system_packages() {
  if [ "$#" -eq 0 ]; then
    return 0
  fi

  if has_cmd apt-get; then
    sudo_cmd apt-get update
    sudo_cmd apt-get install -y "$@"
  elif has_cmd dnf; then
    sudo_cmd dnf install -y "$@"
  elif has_cmd yum; then
    sudo_cmd yum install -y "$@"
  elif has_cmd pacman; then
    sudo_cmd pacman -Sy --noconfirm "$@"
  elif has_cmd apk; then
    sudo_cmd apk add --no-cache "$@"
  elif has_cmd brew; then
    brew install "$@"
  else
    die "no supported package manager found; missing packages: $*"
  fi
}

ensure_fetcher() {
  if has_cmd curl || has_cmd wget; then
    return 0
  fi
  log "curl/wget not found; installing curl"
  install_system_packages curl
}

fetch_stdout() {
  if has_cmd curl; then
    curl -fsSL "$1"
  elif has_cmd wget; then
    wget -qO- "$1"
  else
    die "curl or wget is required"
  fi
}

fetch_file() {
  if has_cmd curl; then
    curl -fL "$1" -o "$2"
  elif has_cmd wget; then
    wget -O "$2" "$1"
  else
    die "curl or wget is required"
  fi
}

ensure_base_tools() {
  local missing=()
  has_cmd tar || missing+=(tar)
  has_cmd uname || missing+=(coreutils)
  if [ "${#missing[@]}" -gt 0 ]; then
    log "base tools missing; installing: ${missing[*]}"
    install_system_packages "${missing[@]}"
  fi
  ensure_fetcher
}

detect_platform() {
  local os arch
  case "$(uname -s)" in
    Linux) os="linux" ;;
    Darwin) os="darwin" ;;
    *) die "unsupported OS: $(uname -s)" ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac

  printf '%s_%s' "$os" "$arch"
}

latest_version() {
  local json tag
  json="$(fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest")"
  tag="$(printf '%s\n' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$tag" ] || die "failed to detect latest release for ${REPO}"
  printf '%s' "$tag"
}

install_lsp_bridge_binary() {
  local version platform asset url tmp archive

  if [ "$VERSION" = "latest" ]; then
    version="$(latest_version)"
  else
    version="$VERSION"
  fi

  platform="$(detect_platform)"
  asset="lsp-bridge_${version}_${platform}.tar.gz"
  url="https://github.com/${REPO}/releases/download/${version}/${asset}"

  log "install root: ${INSTALL_ROOT}"
  log "latest version: ${version}"
  log "downloading: ${url}"

  mkdir -p "$BIN_DIR"
  tmp="$(mktemp -d)"
  CLEANUP_DIR="$tmp"
  archive="${tmp}/${asset}"

  fetch_file "$url" "$archive"
  tar -xzf "$archive" -C "$tmp"

  if [ -f "${tmp}/lsp-bridge" ]; then
    install -m 0755 "${tmp}/lsp-bridge" "${BIN_DIR}/lsp-bridge"
  elif [ -f "${tmp}/bin/lsp-bridge" ]; then
    install -m 0755 "${tmp}/bin/lsp-bridge" "${BIN_DIR}/lsp-bridge"
  else
    die "release asset does not contain lsp-bridge binary"
  fi

  log "installed lsp-bridge to ${BIN_DIR}/lsp-bridge"
}

ensure_node_npm() {
  if has_cmd node && has_cmd npm; then
    return 0
  fi

  log "node/npm not found; installing"
  if has_cmd apt-get; then
    install_system_packages nodejs npm
  elif has_cmd dnf || has_cmd yum || has_cmd pacman || has_cmd apk; then
    install_system_packages nodejs npm
  elif has_cmd brew; then
    install_system_packages node
  else
    die "node/npm are required for pyright-langserver"
  fi
}

install_pyright() {
  if [ "$INSTALL_PYRIGHT" != "1" ]; then
    return 0
  fi

  if has_cmd pyright-langserver || [ -x "${BIN_DIR}/pyright-langserver" ]; then
    log "pyright-langserver already available"
    return 0
  fi

  ensure_node_npm
  log "installing pyright into ${NODE_PREFIX}"
  mkdir -p "$NODE_PREFIX" "$BIN_DIR"
  npm install --prefix "$NODE_PREFIX" pyright@latest
  ln -sf "${NODE_PREFIX}/node_modules/.bin/pyright-langserver" "${BIN_DIR}/pyright-langserver"
  ln -sf "${NODE_PREFIX}/node_modules/.bin/pyright" "${BIN_DIR}/pyright"
}

ensure_go() {
  if has_cmd go; then
    return 0
  fi

  log "go not found; installing"
  if has_cmd apt-get; then
    install_system_packages golang-go
  elif has_cmd dnf || has_cmd yum || has_cmd pacman || has_cmd apk || has_cmd brew; then
    install_system_packages go
  else
    die "go is required for gopls"
  fi
}

install_gopls() {
  if [ "$INSTALL_GOPLS" != "1" ]; then
    return 0
  fi

  if has_cmd gopls || [ -x "${BIN_DIR}/gopls" ]; then
    log "gopls already available"
    return 0
  fi

  ensure_go
  log "installing gopls into ${BIN_DIR}"
  mkdir -p "$BIN_DIR"
  GOBIN="$BIN_DIR" go install golang.org/x/tools/gopls@latest
}

shell_rc_file() {
  case "${SHELL:-}" in
    */zsh) printf '%s/.zshrc' "$HOME" ;;
    */bash) printf '%s/.bashrc' "$HOME" ;;
    */fish) printf '%s/.config/fish/config.fish' "$HOME" ;;
    *) printf '%s/.profile' "$HOME" ;;
  esac
}

update_shell_env() {
  local rc marker_start marker_end tmp
  rc="${LSP_BRIDGE_SHELL_RC:-$(shell_rc_file)}"
  marker_start="# >>> lsp-bridge >>>"
  marker_end="# <<< lsp-bridge <<<"

  mkdir -p "$(dirname "$rc")"
  touch "$rc"

  tmp="$(mktemp)"
  sed "/${marker_start}/,/${marker_end}/d" "$rc" > "$tmp"

  {
    cat "$tmp"
    printf '\n%s\n' "$marker_start"
    if [ "${SHELL:-}" != "${SHELL%fish}" ]; then
      printf 'set -gx LSP_BRIDGE_HOME "%s"\n' "$INSTALL_ROOT"
      printf 'fish_add_path "%s"\n' "$BIN_DIR"
    else
      printf 'export LSP_BRIDGE_HOME="%s"\n' "$INSTALL_ROOT"
      printf 'case ":$PATH:" in *":$LSP_BRIDGE_HOME/bin:"*) ;; *) export PATH="$LSP_BRIDGE_HOME/bin:$PATH" ;; esac\n'
    fi
    printf '%s\n' "$marker_end"
  } > "$rc"

  rm -f "$tmp"
  export LSP_BRIDGE_HOME="$INSTALL_ROOT"
  export PATH="$BIN_DIR:$PATH"
  log "updated shell environment in ${rc}"
}

main() {
  ensure_base_tools
  install_lsp_bridge_binary
  install_pyright
  install_gopls
  update_shell_env

  log "done"
  log "open a new shell or run: source \"$(shell_rc_file)\""
  log "binary: ${BIN_DIR}/lsp-bridge"
}

main "$@"
