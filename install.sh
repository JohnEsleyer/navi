#!/usr/bin/env bash
#
# install.sh - build and install the Navi CLI (and desktop GUI, if possible).
#
#   ./install.sh                 install to ~/.local/bin (no sudo needed)
#   ./install.sh --system        install to /usr/local/bin (needs sudo on the install step)
#   ./install.sh --prefix DIR    install to DIR
#   ./install.sh --uninstall     remove an existing install from the target dir
#   ./install.sh --no-gui        skip building the desktop GUI
#   ./install.sh --help          show this help
#
# The CLI is always required. The GUI needs cgo + GTK/WebKit system libraries,
# so its build is best-effort: if the toolchain or libs are missing, the GUI is
# skipped with a warning and the CLI still installs.

set -euo pipefail

MIN_GO="1.26"
BIN_NAME="navi"
GUI_NAME="navi-gui"

# Resolve repo root from this script's location, so it works from anywhere.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TARGET_PREFIX="$HOME/.local/bin"
WANT_GUI=1
UNINSTALL=0

# ---------------------------------------------------------------- output ----

if [ -t 1 ]; then
  C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'
  C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_CYAN=$'\033[36m'
else
  C_RESET=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_CYAN=""
fi

info()  { printf '%s\n' "${C_GREEN}==>${C_RESET} $*"; }
step()  { printf '%s\n' "${C_CYAN} ->${C_RESET} $*"; }
warn()  { printf '%s\n' "${C_YELLOW}warning:${C_RESET} $*" >&2; }
die()   { printf '%s\n' "${C_RED}error:${C_RESET} $*" >&2; exit 1; }

usage() {
  sed -n '2,15p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 0
}

# ----------------------------------------------------------------- args ----

while [ $# -gt 0 ]; do
  case "$1" in
    --system)     TARGET_PREFIX="/usr/local/bin" ;;
    --prefix)     shift; [ $# -gt 0 ] || die "--prefix requires a directory"; TARGET_PREFIX="$1" ;;
    --prefix=*)   TARGET_PREFIX="${1#--prefix=}" ;;
    --no-gui)     WANT_GUI=0 ;;
    --uninstall)  UNINSTALL=1 ;;
    -h|--help)    usage ;;
    *)            die "unknown option: $1 (try --help)" ;;
  esac
  shift
done

# ------------------------------------------------------------ uninstall ----

if [ "$UNINSTALL" -eq 1 ]; then
  info "Uninstalling from $TARGET_PREFIX"
  removed=0
  for name in "$BIN_NAME" "$GUI_NAME"; do
    if [ -e "$TARGET_PREFIX/$name" ]; then
      rm -f "$TARGET_PREFIX/$name" || die "could not remove $TARGET_PREFIX/$name (try sudo?)"
      step "removed $TARGET_PREFIX/$name"
      removed=1
    fi
  done
  [ "$removed" -eq 1 ] || info "Nothing to remove."
  exit 0
fi

# ------------------------------------------------------------ preflight ----

cd "$REPO_ROOT"
[ -f go.mod ] || die "no go.mod in $REPO_ROOT - is this the Navi repo?"

command -v go >/dev/null 2>&1 || die "Go toolchain not found on PATH. Install Go $MIN_GO+ first."

GO_VERSION="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
[ -n "$GO_VERSION" ] || die "could not determine Go version"

if ! printf '%s\n%s\n' "$MIN_GO" "$GO_VERSION" | sort -V -C; then
  die "Go $GO_VERSION is too old; Navi needs $MIN_GO+"
fi

info "Navi installer"
step "repo:     $REPO_ROOT"
step "go:       $GO_VERSION"
step "target:   $TARGET_PREFIX"

if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || true)"
  [ -n "$VERSION" ] && step "version:  $VERSION"
fi

# ---------------------------------------------------------------- build ----

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

info "Building $BIN_NAME (CLI)"
GOFLAGS="-trimpath" go build -o "$WORKDIR/$BIN_NAME" ./cmd/navi \
  || die "CLI build failed"
step "ok: $WORKDIR/$BIN_NAME"

BUILT_GUI=0
if [ "$WANT_GUI" -eq 1 ]; then
  info "Building $GUI_NAME (desktop GUI, best-effort)"
  if GOFLAGS="-trimpath" go build -o "$WORKDIR/$GUI_NAME" ./cmd/navi-gui 2>"$WORKDIR/gui-build.log"; then
    BUILT_GUI=1
    step "ok: $WORKDIR/$GUI_NAME"
  else
    warn "GUI build failed - installing the CLI only."
    warn "The GUI needs cgo plus GTK/WebKit headers. On Debian/Ubuntu:"
    warn "  sudo apt install build-essential libgtk-3-dev libwebkit2gtk-4.1-dev"
    if [ "${NAVI_DEBUG:-0}" = "1" ]; then
      printf '%s\n' "--- gui build log ---"
      cat "$WORKDIR/gui-build.log"
    fi
  fi
else
  step "skipped (--no-gui)"
fi

# -------------------------------------------------------------- install ----

if [ ! -d "$TARGET_PREFIX" ]; then
  step "creating $TARGET_PREFIX"
  mkdir -p "$TARGET_PREFIX" 2>/dev/null \
    || die "cannot create $TARGET_PREFIX (re-run with sudo, or use --prefix)"
fi

if [ ! -w "$TARGET_PREFIX" ]; then
  die "$TARGET_PREFIX is not writable. Re-run the install with sudo, or choose a user-writable location:\n    ./install.sh --prefix \"\$HOME/.local/bin\""
fi

info "Installing to $TARGET_PREFIX"
install -m 0755 "$WORKDIR/$BIN_NAME" "$TARGET_PREFIX/$BIN_NAME"
step "installed $TARGET_PREFIX/$BIN_NAME"

if [ "$BUILT_GUI" -eq 1 ]; then
  install -m 0755 "$WORKDIR/$GUI_NAME" "$TARGET_PREFIX/$GUI_NAME"
  step "installed $TARGET_PREFIX/$GUI_NAME"
fi

# ---------------------------------------------------------------- verify ---

# Smoke-test without executing the REPL: `navi` has no flag parsing and would
# treat "--help" as a prompt, so inspect the binary instead of running it.
if command -v go >/dev/null 2>&1; then
  go version -m "$TARGET_PREFIX/$BIN_NAME" >/dev/null 2>&1 \
    || warn "$TARGET_PREFIX/$BIN_NAME does not look like a valid Go binary"
fi

# The /gui command looks for the GUI next to the CLI, then on PATH - both are
# satisfied when they share a directory.
if [ "$BUILT_GUI" -eq 1 ] && [ "$(dirname "$TARGET_PREFIX/$GUI_NAME")" != "$(dirname "$TARGET_PREFIX/$BIN_NAME")" ]; then
  warn "CLI and GUI are in different directories; /gui may not find the GUI."
fi

# ----------------------------------------------------------------- PATH ----

case ":$PATH:" in
  *":$TARGET_PREFIX:"*) ;;
  *)
    warn "$TARGET_PREFIX is not on your PATH."
    case "$SHELL" in
      */zsh)   rc="~/.zshrc" ;;
      */fish)  rc="~/.config/fish/config.fish" ;;
      *)       rc="~/.bashrc" ;;
    esac
    if [ "$rc" = "~/.config/fish/config.fish" ]; then
      printf '    Add it with:  set -U fish_user_paths %s \$fish_user_paths\n' "$TARGET_PREFIX"
    else
      printf "    Add it with:  echo 'export PATH=\"%s:\$PATH\"' >> %s\n" "$TARGET_PREFIX" "$rc"
    fi
    ;;
esac

# ---------------------------------------------------------------- summary --

printf '\n%s\n' "${C_BOLD}Installed.${C_RESET}"
printf '  %snavi%s        %s\n' "$C_BOLD" "$C_RESET" "$TARGET_PREFIX/$BIN_NAME"
if [ "$BUILT_GUI" -eq 1 ]; then
  printf '  %snavi-gui%s    %s\n' "$C_BOLD" "$C_RESET" "$TARGET_PREFIX/$GUI_NAME"
else
  printf '  %snavi-gui%s    %s(skipped)%s\n' "$C_BOLD" "$C_RESET" "$C_YELLOW" "$C_RESET"
fi

cat <<EOF

Next steps:
  export NAVI_API_KEY=...         # your Groq/OpenAI/DeepSeek key
  navi                            # start a session
  navi                            # then type /gui inside the session,
                                  # or run: navi-gui

Uninstall:
  $REPO_ROOT/install.sh --uninstall ${TARGET_PREFIX/#$HOME/\~}
EOF
