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
    # Disable lintian during the build. Upstream Cuttlefish packages trip many
    # lintian policy checks (bad changelog, Bazel runfiles in /usr/bin, CDN
    # links, etc.). Those are cosmetic and emit E:/W: tags that make lintian
    # exit non-zero, which would otherwise abort the build even though the .deb
    # files were produced successfully.
    build_rc=0
    (
      cd "$build_dir"
      # debuild reads DEBUILD_LINTIAN from a devscripts config file, so write a
      # throwaway HOME config that turns lintian off for this build only.
      tmp_home="$(mktemp -d)"
      printf 'DEBUILD_LINTIAN=no\n' >"${tmp_home}/.devscripts"
      HOME="$tmp_home" DEB_CHECK_COMMAND="" tools/buildutils/build_packages.sh
    ) || build_rc=$?

    # Trust the produced artifacts rather than the build script's exit code:
    # lintian failures (and other non-fatal post-build steps) must not block
    # installation if the packages we need actually exist.
    shopt -s nullglob
    base_debs=("${build_dir}"/cuttlefish-base_*_*.deb)
    user_debs=("${build_dir}"/cuttlefish-user_*_*.deb)
    shopt -u nullglob

    if [[ ${#base_debs[@]} -eq 0 || ${#user_debs[@]} -eq 0 ]]; then
      echo "ERROR: Cuttlefish package build did not produce the expected .deb files (exit ${build_rc})." >&2
      echo "       Inspect the build output in: ${build_dir}" >&2
      echo "       To skip Cuttlefish and run the OpenCuttles dashboard only, re-run with:" >&2
      echo "         OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1 bash scripts/ubuntu/quickstart.sh" >&2
      exit 1
    fi

    if [[ "$build_rc" -ne 0 ]]; then
      echo "Cuttlefish build script exited ${build_rc} (likely lintian policy warnings); .deb files were produced, continuing."
    fi

    echo "Installing Cuttlefish Debian packages..."
    sudo apt-get install -y "${base_debs[@]}" "${user_debs[@]}"
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
# on :8443. Make sure the service (shipped with cuttlefish-base/user) is running
# so OpenCuttles can proxy device consoles.
for svc in cuttlefish-operator cuttlefish-host-resources; do
  if systemctl list-unit-files 2>/dev/null | grep -q "^${svc}\b"; then
    sudo systemctl enable --now "$svc" 2>/dev/null || true
  fi
done

echo
echo "Android tools installation complete."
echo "If Cuttlefish was installed for the first time, reboot or log out/in so group membership takes effect."
