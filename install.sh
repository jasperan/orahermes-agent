#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# orahermes-agent — One-Command Installer
# orahermes-agent
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jasperan/orahermes-agent/main/install.sh | bash
#
# Override install location:
#   PROJECT_DIR=/opt/myapp curl -fsSL ... | bash
# ============================================================

REPO_URL="https://github.com/jasperan/orahermes-agent.git"
PROJECT="orahermes-agent"
BRANCH="main"
INSTALL_DIR="${PROJECT_DIR:-$(pwd)/$PROJECT}"

# ── Colors ──────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()    { echo -e "${BLUE}→${NC} $1"; }
success() { echo -e "${GREEN}✓${NC} $1"; }
warn()    { echo -e "${YELLOW}!${NC} $1"; }
fail()    { echo -e "${RED}✗ $1${NC}"; exit 1; }
command_exists() { command -v "$1" &>/dev/null; }

print_banner() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BOLD}  orahermes-agent${NC}"
    echo -e "  orahermes-agent"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

clone_repo() {
    if [ -d "$INSTALL_DIR" ]; then
        warn "Directory $INSTALL_DIR already exists"
        info "Pulling latest changes..."
        (cd "$INSTALL_DIR" && git pull origin "$BRANCH" 2>/dev/null) || true
    else
        info "Cloning repository..."
        git clone --depth 1 -b "$BRANCH" "$REPO_URL" "$INSTALL_DIR" || fail "Clone failed. Check your internet connection."
    fi
    success "Repository ready at $INSTALL_DIR"
}

check_prereqs() {
    info "Checking prerequisites..."
    command_exists git || fail "Git is required — https://git-scm.com/"
    success "Git $(git --version | cut -d' ' -f3)"

    PYTHON=""
    for cmd in python3 python; do
        if command_exists "$cmd"; then
            ver=$("$cmd" -c 'import sys; v=sys.version_info; print(f"{v.major}.{v.minor}")' 2>/dev/null) || continue
            major=${ver%%.*}
            minor=${ver##*.}
            if [ "$major" -gt 3 ] || { [ "$major" -eq 3 ] && [ "$minor" -ge 10 ]; }; then
                PYTHON="$cmd"
                break
            fi
        fi
    done
    [ -n "$PYTHON" ] || fail "Python 3.10+ is required — https://www.python.org/downloads/"
    success "Python $($PYTHON --version | cut -d' ' -f2)"
}

install_deps() {
    cd "$INSTALL_DIR"
    if [ ! -d ".venv" ]; then
        info "Creating virtual environment..."
        $PYTHON -m venv .venv
    else
        info "Using existing virtual environment..."
    fi
    # shellcheck disable=SC1091
    source .venv/bin/activate

    info "Installing dependencies..."
    pip install --upgrade pip -q
    pip install -e ".[dev]" -q 2>/dev/null || pip install -e . -q || {
        if [ -f requirements.txt ]; then
            pip install -r requirements.txt -q
        else
            fail "Could not install dependencies"
        fi
    }
    success "Dependencies installed"
}

main() {
    print_banner
    check_prereqs
    clone_repo
    install_deps
    print_done
}

print_done() {
    echo ""
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "  ${BOLD}Installation complete!${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "  ${BOLD}Location:${NC}  $INSTALL_DIR"
    echo -e "  ${BOLD}Activate:${NC}  source $INSTALL_DIR/.venv/bin/activate"
    echo -e "  ${BOLD}Commands:${NC}  orahermes, hermes, hermes-agent"
    echo ""
    echo -e "  ${BOLD}Note:${NC}     Oracle Database connection required — see README for setup"
    echo ""
}

main "$@"
