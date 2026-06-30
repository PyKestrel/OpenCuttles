#!/usr/bin/env bash
set -euo pipefail

# Installs ADB and the Cuttlefish host tools (cvd + the WebRTC operator).
#
# Strategy (fast path first):
#   1. Google Artifact Registry APT repo  -> apt-get install cuttlefish-base cuttlefish-user
#   2. Prebuilt .deb assets from the latest android-cuttlefish GitHub release
#   3. Build from source (only if OPENCUTTLES_BUILD_CUTTLEFISH_FROM_SOURCE=1)
#
# The first two paths install in seconds and need almost no disk. Source builds
# require ~30-40 GB and many minutes, so they are opt-in.

skip_cuttlefish="${OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL:-0}"
build_from_source="${OPENCUTTLES_BUILD_CUTTLEFISH_FROM_SOURCE:-0}"
build_dir="${OPENCUTTLES_CUTTLEFISH_BUILD_DIR:-/tmp/opencuttles-cuttlefish-build}"
repo_url="${OPENCUTTLES_CUTTLEFISH_REPO:-https://github.com/google/android-cuttlefish.git}"
gh_repo="${OPENCUTTLES_CUTTLEFISH_GH_REPO:-google/android-cuttlefish}"
min_build_gb="${OPENCUTTLES_CUTTLEFISH_MIN_DISK_GB:-40}"

echo "Installing Android platform tools..."
sudo apt-get install -y android-tools-adb

cuttlefish_present() {
  command -v cvd >/dev/null 2>&1 || command -v launch_cvd >/dev/null 2>&1
}

# --- path 1: Artifact Registry APT repo -------------------------------------
install_from_apt_repo() {
  local key_url="https://us-apt.pkg.dev/doc/repo-signing-key.gpg"
  local list="/etc/apt/sources.list.d/android-cuttlefish.list"
  echo "Trying prebuilt Cuttlefish packages from Google Artifact Registry..."
  if ! sudo curl -fsSL "$key_url" -o /etc/apt/trusted.gpg.d/android-cuttlefish.asc; then
    echo "Could not fetch the Artifact Registry signing key."
    return 1
  fi
  sudo chmod a+r /etc/apt/trusted.gpg.d/android-cuttlefish.asc
  echo "deb https://us-apt.pkg.dev/projects/android-cuttlefish-artifacts android-cuttlefish main" \
    | sudo tee "$list" >/dev/null
  if ! sudo apt-get update; then
    echo "apt-get update failed for the Cuttlefish repo; removing it."
    sudo rm -f "$list"
    return 1
  fi
  if sudo apt-get install -y cuttlefish-base cuttlefish-user; then
    return 0
  fi
  sudo rm -f "$list"
  return 1
}

# --- path 2: prebuilt .deb from the latest GitHub release -------------------
install_from_github_release() {
  echo "Trying prebuilt Cuttlefish .deb assets from the latest ${gh_repo} release..."
  local arch deb_arch tmp api urls
  arch="$(dpkg --print-architecture)"
  case "$arch" in
    amd64) deb_arch="amd64" ;;
    arm64) deb_arch="arm64" ;;
    *) echo "No prebuilt release assets for architecture ${arch}."; return 1 ;;
  esac

  tmp="$(mktemp -d)"
  api="https://api.github.com/repos/${gh_repo}/releases/latest"
  # Match cuttlefish-base_* and cuttlefish-user_* assets for this arch.
  urls="$(curl -fsSL "$api" \
    | grep -oE '"browser_download_url": *"[^"]+"' \
    | sed -E 's/.*"browser_download_url": *"([^"]+)".*/\1/' \
    | grep -E "cuttlefish-(base|user)_.*_${deb_arch}\.deb$" || true)"

  if [[ -z "$urls" ]]; then
    echo "No matching Cuttlefish .deb assets found in the latest release."
    rm -rf "$tmp"
    return 1
  fi

  local url
  while IFS= read -r url; do
    [[ -z "$url" ]] && continue
    echo "Downloading $(basename "$url")"
    if ! curl -fL "$url" -o "${tmp}/$(basename "$url")"; then
      echo "Download failed: ${url}"
      rm -rf "$tmp"
      return 1
    fi
  done <<<"$urls"

  if sudo apt-get install -y "${tmp}"/cuttlefish-base_*.deb "${tmp}"/cuttlefish-user_*.deb; then
    rm -rf "$tmp"
    return 0
  fi
  rm -rf "$tmp"
  return 1
}

# --- path 3: build from source (opt-in, disk-gated) -------------------------
install_from_source() {
  if [[ "$build_from_source" != "1" ]]; then
    echo "ERROR: Could not install prebuilt Cuttlefish packages." >&2
    echo "       Re-run with OPENCUTTLES_BUILD_CUTTLEFISH_FROM_SOURCE=1 to build from source (~${min_build_gb} GB, slow)," >&2
    echo "       or OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1 to run the dashboard without live launches." >&2
    return 1
  fi

  local avail_gb
  avail_gb="$(df -BG --output=avail "$(dirname "$build_dir")" 2>/dev/null | tail -1 | tr -dc '0-9' || echo 0)"
  if [[ -n "$avail_gb" && "$avail_gb" -lt "$min_build_gb" ]]; then
    echo "ERROR: Only ${avail_gb} GB free near ${build_dir}; the source build needs ~${min_build_gb} GB." >&2
    echo "       Free space (rm -rf ~/.cache/bazel ${build_dir}; sudo apt-get clean) or set OPENCUTTLES_CUTTLEFISH_BUILD_DIR to a larger volume." >&2
    return 1
  fi

  echo "Building Cuttlefish Debian packages from source. This can take a while..."
  sudo apt-get install -y git devscripts equivs config-package-dev debhelper-compat golang curl

  rm -rf "$build_dir"
  git clone --depth 1 "$repo_url" "$build_dir"

  local build_rc=0
  (
    cd "$build_dir"
    # Disable lintian; upstream trips many cosmetic policy checks that would
    # otherwise make the build exit non-zero even when .deb files were produced.
    tmp_home="$(mktemp -d)"
    printf 'DEBUILD_LINTIAN=no\n' >"${tmp_home}/.devscripts"
    HOME="$tmp_home" DEB_CHECK_COMMAND="" tools/buildutils/build_packages.sh
  ) || build_rc=$?

  shopt -s nullglob
  local base_debs=("${build_dir}"/cuttlefish-base_*_*.deb)
  local user_debs=("${build_dir}"/cuttlefish-user_*_*.deb)
  shopt -u nullglob

  if [[ ${#base_debs[@]} -eq 0 || ${#user_debs[@]} -eq 0 ]]; then
    echo "ERROR: Source build did not produce the expected .deb files (exit ${build_rc})." >&2
    echo "       Inspect the build output in: ${build_dir}" >&2
    return 1
  fi
  if [[ "$build_rc" -ne 0 ]]; then
    echo "Build script exited ${build_rc} (likely lintian warnings); .deb files were produced, continuing."
  fi
  sudo apt-get install -y "${base_debs[@]}" "${user_debs[@]}"
}

if cuttlefish_present; then
  echo "Cuttlefish host tools already installed."
elif [[ "$skip_cuttlefish" == "1" ]]; then
  echo "Skipping Cuttlefish install because OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1."
else
  if install_from_apt_repo || install_from_github_release || install_from_source; then
    echo "Cuttlefish host tools installed."
  else
    exit 1
  fi
fi

target_user="${SUDO_USER:-$USER}"
if id "$target_user" >/dev/null 2>&1; then
  sudo usermod -aG kvm "$target_user" || true
  sudo usermod -aG render "$target_user" || true
  sudo usermod -aG cvdnetwork "$target_user" 2>/dev/null || true
fi

sudo usermod -aG kvm opencuttles 2>/dev/null || true
sudo usermod -aG render opencuttles 2>/dev/null || true
sudo usermod -aG cvdnetwork opencuttles 2>/dev/null || true

# The interactive WebRTC console is served by the host-wide cuttlefish-operator
# on :1443 (older builds :8443). Make sure the service (shipped with cuttlefish-base/user) is running
# so OpenCuttles can proxy device consoles.
for svc in cuttlefish-operator cuttlefish-host-resources; do
  if systemctl list-unit-files 2>/dev/null | grep -q "^${svc}\b"; then
    sudo systemctl enable --now "$svc" 2>/dev/null || true
  fi
done

echo
echo "Android tools installation complete."
echo "If Cuttlefish was installed for the first time, reboot or log out/in so group membership takes effect."
