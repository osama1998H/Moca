#!/bin/sh
# Moca Framework Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/osama1998H/moca/main/install.sh | sh
#
# Environment variables:
#   MOCA_VERSION      - Version to install (default: latest release)
#   MOCA_INSTALL_DIR  - Installation directory (default: /usr/local/bin or ~/.local/bin)

set -e

GITHUB_ORG="osama1998H"
GITHUB_REPO="moca"
GITHUB_BASE="https://github.com/${GITHUB_ORG}/${GITHUB_REPO}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info()  { printf "  \033[1;34m>\033[0m %s\n" "$1"; }
ok()    { printf "  \033[1;32m>\033[0m %s\n" "$1"; }
warn()  { printf "  \033[1;33m>\033[0m %s\n" "$1"; }
err()   { printf "  \033[1;31merror:\033[0m %s\n" "$1" >&2; }
die()   { err "$1"; exit 1; }

banner() {
    printf "\n"
    printf "  \033[1;36m__  __                  \033[0m\n"
    printf "  \033[1;36m|  \/  | ___   ___ __ _ \033[0m\n"
    printf "  \033[1;36m| |\/| |/ _ \ / __/ _\` |\033[0m\n"
    printf "  \033[1;36m| |  | | (_) | (_| (_| |\033[0m\n"
    printf "  \033[1;36m|_|  |_|\___/ \___\__,_|\033[0m\n"
    printf "\n"
    printf "  \033[1mMoca Framework Installer\033[0m\n"
    printf "\n"
}

# ---------------------------------------------------------------------------
# Platform detection
# ---------------------------------------------------------------------------

detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux"  ;;
        Darwin*) echo "darwin" ;;
        *)       die "Unsupported operating system: $(uname -s). Moca supports Linux and macOS." ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              die "Unsupported architecture: $(uname -m). Moca supports amd64 and arm64." ;;
    esac
}

# ---------------------------------------------------------------------------
# Download helper — prefers curl, falls back to wget
# ---------------------------------------------------------------------------

has_cmd() { command -v "$1" >/dev/null 2>&1; }

download() {
    url="$1"
    output="$2"
    if has_cmd curl; then
        curl -fsSL -o "$output" "$url"
    elif has_cmd wget; then
        wget -qO "$output" "$url"
    else
        die "Neither curl nor wget found. Please install one and try again."
    fi
}

# ---------------------------------------------------------------------------
# SHA-256 verification helper
# ---------------------------------------------------------------------------

sha256_verify() {
    file="$1"
    expected="$2"

    if has_cmd sha256sum; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif has_cmd shasum; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        warn "Neither sha256sum nor shasum found — skipping checksum verification."
        return 0
    fi

    if [ "$actual" != "$expected" ]; then
        die "Checksum mismatch for $(basename "$file").\n  Expected: ${expected}\n  Got:      ${actual}"
    fi
}

# ---------------------------------------------------------------------------
# Resolve install directory
# ---------------------------------------------------------------------------

resolve_install_dir() {
    if [ -n "$MOCA_INSTALL_DIR" ]; then
        echo "$MOCA_INSTALL_DIR"
        return
    fi

    default_dir="/usr/local/bin"
    if [ -d "$default_dir" ] && [ -w "$default_dir" ]; then
        echo "$default_dir"
    else
        fallback="${HOME}/.local/bin"
        warn "${default_dir} is not writable. Installing to ${fallback} instead."
        warn "To install system-wide, re-run with: curl -fsSL ... | sudo sh"
        echo "$fallback"
    fi
}

# ---------------------------------------------------------------------------
# Resolve version (latest release or user-specified)
# ---------------------------------------------------------------------------

resolve_version() {
    if [ -n "$MOCA_VERSION" ]; then
        # Strip leading 'v' if present so we normalise later
        echo "$MOCA_VERSION" | sed 's/^v//'
        return
    fi

    info "Fetching latest release tag..."
    # GitHub redirects /releases/latest to /releases/tag/vX.Y.Z
    if has_cmd curl; then
        tag=$(curl -fsSI "${GITHUB_BASE}/releases/latest" 2>/dev/null \
              | grep -i '^location:' | sed 's|.*/tag/||;s/[[:space:]]//g')
    elif has_cmd wget; then
        tag=$(wget --spider -S "${GITHUB_BASE}/releases/latest" 2>&1 \
              | grep -i 'Location:' | tail -1 | sed 's|.*/tag/||;s/[[:space:]]//g')
    fi

    if [ -z "$tag" ]; then
        die "Could not determine the latest release. Set MOCA_VERSION and try again."
    fi

    echo "$tag" | sed 's/^v//'
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    banner

    os=$(detect_os)
    arch=$(detect_arch)
    info "Detected platform: ${os}/${arch}"

    version=$(resolve_version)
    info "Installing Moca v${version}"

    install_dir=$(resolve_install_dir)
    mkdir -p "$install_dir"

    archive="moca_${version}_${os}_${arch}.tar.gz"
    archive_url="${GITHUB_BASE}/releases/download/v${version}/${archive}"
    checksums_url="${GITHUB_BASE}/releases/download/v${version}/checksums.txt"

    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT

    # Download archive and checksums
    info "Downloading ${archive}..."
    download "$archive_url"  "$tmp/${archive}"

    info "Downloading checksums.txt..."
    download "$checksums_url" "$tmp/checksums.txt"

    # Verify checksum
    info "Verifying SHA-256 checksum..."
    expected=$(grep "${archive}" "$tmp/checksums.txt" | awk '{print $1}')
    if [ -z "$expected" ]; then
        die "Archive ${archive} not found in checksums.txt."
    fi
    sha256_verify "$tmp/${archive}" "$expected"
    ok "Checksum verified."

    # Extract binaries
    info "Extracting binaries to ${install_dir}..."
    tar -xzf "$tmp/${archive}" -C "$tmp"

    binaries="moca moca-server moca-worker moca-scheduler moca-outbox"
    for bin in $binaries; do
        if [ -f "$tmp/${bin}" ]; then
            mv "$tmp/${bin}" "${install_dir}/${bin}"
            chmod +x "${install_dir}/${bin}"
        else
            warn "Binary '${bin}' not found in archive — skipped."
        fi
    done

    ok "Moca v${version} installed successfully."
    printf "\n"
    info "Install directory: ${install_dir}"
    info "Binaries: ${binaries}"

    # Check PATH
    case ":${PATH}:" in
        *":${install_dir}:"*) ;;
        *)
            printf "\n"
            warn "${install_dir} is not in your PATH."
            warn "Add it by appending this line to your shell profile:"
            printf "\n"
            printf "    export PATH=\"%s:\$PATH\"\n" "$install_dir"
            printf "\n"
            ;;
    esac

    printf "\n"
    ok "Run 'moca --help' to get started."
    printf "\n"
}

main
