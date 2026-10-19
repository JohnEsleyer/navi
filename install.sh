#!/usr/bin/env bash
#
# install.sh - build and install the Navi CLI (and desktop GUI, if wanted).
#
#   ./install.sh                 install to ~/.local/bin (asks about the GUI)
#   ./install.sh --with-gui      include the desktop GUI
#   ./install.sh --no-gui        skip the desktop GUI
#   ./install.sh --system        install to /usr/local/bin (needs sudo on the install step)
#   ./install.sh --prefix DIR    install to DIR
#   ./install.sh --uninstall     remove an existing install from the target dir
#   ./install.sh --no-path       don't edit your shell rc file
#   ./install.sh --help          show this help
#
# The CLI is always required. The GUI needs cgo + GTK/WebKit system libraries.
# With neither --with-gui nor --no-gui, and on a terminal, you are asked which
# you want. Non-interactive runs (CI, pipes) skip the prompt and build the GUI
# best-effort: if the toolchain or libs are missing, the GUI is dropped with a
# warning and the CLI still installs.
#
# Unless --no-path is given, the install directory is added to your shell rc
# file (~/.bashrc, ~/.zshrc or config.fish) inside a marker-delimited block.
# The file is backed up to <rc>.bak first, the block is rewritten rather than
# duplicated on reinstall, and --uninstall removes it.

set -euo pipefail

MIN_GO="1.26"
BIN_NAME="navi"
GUI_NAME="navi-gui"

# Resolve repo root from this script's location, so it works from anywhere.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Resolve the invoking user's real home and login shell. Under sudo, $HOME is
# /root and $SHELL is root's shell, so everything derived from them - the
# default install prefix and the rc file to edit - would target root instead of
# the person running the installer. Follow SUDO_USER to get the right values.
# This has to happen before TARGET_PREFIX is defaulted, or `sudo ./install.sh`
# silently installs to /root/.local/bin, a directory the user cannot read.
REAL_HOME="$HOME"
REAL_SHELL="${SHELL:-bash}"
if [ -n "${SUDO_USER:-}" ]; then
  _pw="$(getent passwd "$SUDO_USER" 2>/dev/null || true)"
  if [ -n "$_pw" ]; then
    REAL_HOME="$(printf '%s' "$_pw" | cut -d: -f6)"
    REAL_SHELL="$(printf '%s' "$_pw" | cut -d: -f7)"
  fi
fi

# Default to the invoking user's home rather than $HOME: under sudo $HOME is
# /root, and installing there would put navi somewhere the user cannot execute
# it from. An explicit --prefix or --system still wins.
TARGET_PREFIX="$REAL_HOME/.local/bin"
# GUI_MODE: "auto" = ask when on a terminal, otherwise build best-effort.
#           "yes" / "no" = forced by --with-gui / --no-gui.
GUI_MODE="auto"
WANT_GUI=1
UNINSTALL=0
SKIP_RC=0

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
  # Print the leading comment block (everything after the shebang up to the
  # first non-comment line), so editing the header can't desync this.
  awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "${BASH_SOURCE[0]}"
  exit 0
}

# GUI dependency probe, used to make the interactive choice informative.
# Prints the missing package names; empty output means the libs are present.
gui_missing_deps() {
  command -v pkg-config >/dev/null 2>&1 || { echo "pkg-config"; return; }
  command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1 || echo "build-essential"
  pkg-config --exists webkit2gtk-4.1 2>/dev/null && return
  pkg-config --exists webkit2gtk-4.0 2>/dev/null && return
  echo "libwebkit2gtk-4.1-dev"
  pkg-config --exists gtk+-3.0 2>/dev/null || echo "libgtk-3-dev"
}

# Resolve the GUI choice once we know whether we're on a terminal.
resolve_gui_choice() {
  case "$GUI_MODE" in
    yes) WANT_GUI=1; return ;;
    no)  WANT_GUI=0; return ;;
  esac

  # Not forced: ask only if both stdin and stdout are a terminal.
  if [ ! -t 0 ] || [ ! -t 1 ]; then
    WANT_GUI=1
    step "gui:      building best-effort (non-interactive)"
    return
  fi

  MISSING="$(gui_missing_deps)"
  if [ -n "$MISSING" ]; then
    printf '%s\n' "${C_YELLOW}The desktop GUI needs system libraries you don't have:${C_RESET} $MISSING"
    printf '    Debian/Ubuntu:  sudo apt install build-essential libgtk-3-dev libwebkit2gtk-4.1-dev\n'
  fi

  printf 'Include the desktop GUI? [%sY/n%s] ' "$C_BOLD" "$C_RESET"
  read -r reply || reply=""
  case "$reply" in
    [Nn]*) WANT_GUI=0 ;;
    *)     WANT_GUI=1 ;;
  esac
}

# ----------------------------------------------------------------- args ----

while [ $# -gt 0 ]; do
  case "$1" in
    --system)     TARGET_PREFIX="/usr/local/bin" ;;
    --prefix)     shift; [ $# -gt 0 ] || die "--prefix requires a directory"; TARGET_PREFIX="$1" ;;
    --prefix=*)   TARGET_PREFIX="${1#--prefix=}" ;;
    --no-gui)     GUI_MODE="no" ;;
    --with-gui)   GUI_MODE="yes" ;;
    --no-path)    SKIP_RC=1 ;;
    --uninstall)  UNINSTALL=1 ;;
    -h|--help)    usage ;;
    *)            die "unknown option: $1 (try --help)" ;;
  esac
  shift
done

# ------------------------------------------------------------ uninstall ----

# The rc file we manage, chosen by the invoking user's login shell.
shell_rc_file() {
  case "$(basename "$REAL_SHELL")" in
    fish) printf '%s\n' "${REAL_HOME}/.config/fish/config.fish" ;;
    zsh)  printf '%s\n' "${REAL_HOME}/.zshrc" ;;
    *)    printf '%s\n' "${REAL_HOME}/.bashrc" ;;
  esac
}

# Insert or refresh a marker-delimited PATH block in the shell rc file.
# A blind `>> ~/.bashrc` would duplicate on every reinstall and be impossible
# to cleanly remove, so the block is bounded by markers and rewritten in place.
# Returns 0 on write, 2 if the prefix is already resolvable.
add_shell_path() {
  local rc_file="$1" prefix="$2"
  local begin="# >>> navi PATH >>>" end="# <<< navi PATH <<<" line tmp rc_dir

  case "$rc_file" in
    */config.fish) line="fish_add_path $prefix" ;;
    *)             line="export PATH=\"$prefix:\$PATH\"" ;;
  esac

  # Nothing to do if the prefix is already resolvable.
  case ":$PATH:" in
    *":$prefix:"*) return 2 ;;
  esac

  rc_dir="$(dirname "$rc_file")"
  [ -d "$rc_dir" ] || mkdir -p "$rc_dir" 2>/dev/null || return 1

  # Back up before the first edit. Never overwrite an existing backup: it may
  # predate this installer and be more valuable than today's copy.
  if [ -f "$rc_file" ] && [ ! -e "$rc_file.bak" ]; then
    cp "$rc_file" "$rc_file.bak" || return 1
  fi

  if [ -f "$rc_file" ] && grep -qF "$begin" "$rc_file"; then
    # Block already present (reinstall, or a different --prefix): rewrite it
    # between the markers instead of appending a second copy.
    tmp="$(mktemp)" || return 1
    awk -v b="$begin" -v e="$end" -v l="$line" '
      $0 == b { print b; print l; skip = 1; next }
      $0 == e { skip = 0; print e; next }
      !skip { print }
    ' "$rc_file" > "$tmp" || { rm -f "$tmp"; return 1; }
    cat "$tmp" > "$rc_file" || { rm -f "$tmp"; return 1; }
    rm -f "$tmp"
  else
    # New block. Terminate a trailing line first so we never glue onto a file
    # whose last line lacks a newline.
    [ -s "$rc_file" ] && [ -n "$(tail -c1 "$rc_file")" ] && printf '\n' >> "$rc_file"
    printf '%s\n%s\n%s\n' "$begin" "$line" "$end" >> "$rc_file"
  fi
  return 0
}

# Remove our block again (used by --uninstall). Leaves everything else intact.
remove_shell_path() {
  local rc_file="$1" tmp
  local begin="# >>> navi PATH >>>" end="# <<< navi PATH <<<"
  [ -f "$rc_file" ] || return 0
  grep -qF "$begin" "$rc_file" || return 0

  tmp="$(mktemp)" || return 1
  awk -v b="$begin" -v e="$end" '
    $0 == b { skip = 1; next }
    $0 == e { skip = 0; next }
    !skip { print }
  ' "$rc_file" > "$tmp" || { rm -f "$tmp"; return 1; }
  cat "$tmp" > "$rc_file" || { rm -f "$tmp"; return 1; }
  rm -f "$tmp"
}

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

  # Drop the PATH block we added, so the rc file is left as we found it.
  RC_FILE="$(shell_rc_file)"
  if remove_shell_path "$RC_FILE"; then
    [ -f "$RC_FILE" ] && step "removed PATH block from $RC_FILE"
  else
    warn "could not clean $RC_FILE; remove the '# >>> navi PATH >>>' block manually."
  fi

  [ "$removed" -eq 1 ] || info "No binaries to remove."
  exit 0
fi

# ------------------------------------------------------------ preflight ----

cd "$REPO_ROOT"
[ -f go.mod ] || die "no go.mod in $REPO_ROOT - is this the Navi repo?"

# Locate a Go toolchain. `command -v go` is tried first so an explicit PATH
# choice always wins, but that is not sufficient on its own: many installs put
# Go on PATH via an rc file (.bashrc, .profile), and those are sourced only by
# interactive shells. A script like this one never sees them, so a perfectly
# working Go looks missing. Fall back to the usual install locations.
#
# Under sudo, $HOME points at /root, so resolve the invoking user's home too.
find_go() {
  if command -v go >/dev/null 2>&1; then
    command -v go
    return 0
  fi

  local real_home="$HOME"
  if [ -n "${SUDO_USER:-}" ]; then
    real_home="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)"
    [ -n "$real_home" ] || real_home="$HOME"
  fi

  local d base candidate best="" best_ver="" best_tier=99 ver tier
  local -a bases=()

  # Versioned hand installs. A downloaded tarball extracts to a nested
  # go/ dir (go1.26.1/go/bin/go), but some people unpack it flat
  # (go1.26.1/bin/go), so both layouts are checked.
  for d in "$real_home"/go[0-9]*.[0-9]* "$real_home"/sdk/go*; do
    [ -d "$d" ] && bases+=("$d" "$d/go")
  done
  # Then the standard system and user locations.
  for d in /usr/local/go /usr/lib/go* /usr/lib/golang /snap/go/current "$real_home"/go /opt/go*; do
    [ -d "$d" ] && bases+=("$d")
  done

  # Paths cannot be ordered to reveal which toolchain is newest - sorting
  # /usr/local/go above ~/go1.26.1 would only reflect that "usr" > "home" as
  # strings. Ask each binary its real version instead. Lower tier wins; ties
  # go to the higher version. Tier 1 is the versioned hand installs.
  for base in "${bases[@]}"; do
    candidate="$base/bin/go"
    [ -x "$candidate" ] || continue

    case "$base" in
      "$real_home"/go[0-9]*.[0-9]*|"$real_home"/go[0-9]*.[0-9]*/go|"$real_home"/sdk/go*) tier=1 ;;
      *) tier=2 ;;
    esac

    ver="$("$candidate" env GOVERSION 2>/dev/null | sed 's/^go//')"
    [ -n "$ver" ] || continue

    if [ "$tier" -lt "$best_tier" ] \
      || { [ "$tier" -eq "$best_tier" ] && [ "$(printf '%s\n%s\n' "$best_ver" "$ver" | sort -V | tail -n1)" = "$ver" ]; }; then
      best="$candidate"; best_ver="$ver"; best_tier="$tier"
    fi
  done

  [ -n "$best" ] || return 1
  printf '%s\n' "$best"
}

GO_BIN="$(find_go)" || die "Go toolchain not found.

  Looked at: PATH, then ~/go*/go/bin, ~/sdk/go*/bin, /usr/local/go/bin,
              /usr/lib/go*/bin, /snap/go/current/bin and /opt/go*/bin.

  Install Go $MIN_GO+ from https://go.dev/dl/, or if it is already installed
  somewhere unusual, run the installer with Go on PATH:
      PATH=/path/to/go/bin:\$PATH ./install.sh"

# Make the resolved toolchain visible to every later `go` invocation (and to
# any subprocesses the GUI/cgo build spawns).
GO_DIR="$(dirname "$GO_BIN")"
case ":$PATH:" in
  *":$GO_DIR:"*) ;;
  *) export PATH="$GO_DIR:$PATH" ;;
esac

[ "$GO_BIN" = "$(command -v go)" ] || step "go:       using $GO_BIN (not on PATH)"

GO_VERSION="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
[ -n "$GO_VERSION" ] || die "could not determine Go version from $GO_BIN"

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

# Decide whether to build the GUI, prompting first if we're on a terminal and
# the user didn't force it with --with-gui / --no-gui.
resolve_gui_choice
if [ "$WANT_GUI" -eq 1 ]; then
  step "gui:      yes"
else
  step "gui:      no"
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
  if [ "$GUI_MODE" = "no" ]; then
    step "skipped (--no-gui)"
  else
    step "skipped (not selected)"
  fi
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

# Under sudo the binaries land owned by root. Inside the invoking user's home
# that is a nuisance: the next plain ./install.sh cannot overwrite them, so the
# user would be forced to keep using sudo forever. Hand ownership back.
if [ -n "${SUDO_USER:-}" ]; then
  if chown "$SUDO_USER" "$TARGET_PREFIX/$BIN_NAME" 2>/dev/null; then
    [ "$BUILT_GUI" -eq 1 ] && chown "$SUDO_USER" "$TARGET_PREFIX/$GUI_NAME" 2>/dev/null || true
    case "$TARGET_PREFIX" in
      "$REAL_HOME"/*) step "owner:    set to $SUDO_USER (avoids needing sudo again)" ;;
    esac
  fi
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

if [ "$SKIP_RC" -eq 1 ]; then
  case ":$PATH:" in
    *":$TARGET_PREFIX:"*) step "path:     $TARGET_PREFIX already on PATH" ;;
    *)                    step "path:     skipped (--no-path); add $TARGET_PREFIX to PATH yourself" ;;
  esac
else
  RC_FILE="$(shell_rc_file)"
  add_shell_path "$RC_FILE" "$TARGET_PREFIX"
  case $? in
    0)  step "path:     added $TARGET_PREFIX to $RC_FILE (backup: $RC_FILE.bak)" ;;
    2)  step "path:     $TARGET_PREFIX already on PATH" ;;
    *)  warn "could not update $RC_FILE; add $TARGET_PREFIX to PATH manually." ;;
  esac
fi

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
