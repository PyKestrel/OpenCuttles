#!/usr/bin/env bash
set -euo pipefail

skip_cuttlefish="${OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL:-0}"
build_dir="${OPENCUTTLES_CUTTLEFISH_BUILD_DIR:-/tmp/opencuttles-cuttlefish-build}"
repo_url="${OPENCUTTLES_CUTTLEFISH_REPO:-https://github.com/google/android-cuttlefish.git}"

echo "Installing Android platform tools..."
sudo apt-get install -y android-tools-adb

if command -v cvd >/dev/null 2>&1 && command -v launch_cvd >/dev/null 2>&1 && command -v stop_cvd >/dev/null 2>&1; then
  echo "Cuttlefish host tools already installed."
else
  if [[ "$skip_cuttlefish" == "1" ]]; then
    echo "Skipping Cuttlefish install because OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1."
  else
    echo "Installing Cuttlefish host build dependencies..."
    sudo apt-get install -y git devscripts equivs config-package-dev debhelper-compat golang curl

    rm -rf "$build_dir"
    git clone --depth 1 "$repo_url" "$build_dir"

    echo "Building Cuttlefish Debian packages. This can take several minutes..."
    (
      cd "$build_dir"
      debuild -i -us -uc -b -d
    )

    echo "Installing Cuttlefish Debian packages..."
    sudo dpkg -i "${build_dir}"/../cuttlefish-base_*_*.deb "${build_dir}"/../cuttlefish-user_*_*.deb || sudo apt-get -f install -y
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

echo
echo "Android tools installation complete."
echo "If Cuttlefish was installed for the first time, reboot or log out/in so group membership takes effect."
