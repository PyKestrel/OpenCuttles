#!/usr/bin/env bash
set -euo pipefail

image_dir="${OPENCUTTLES_DEFAULT_IMAGE_PATH:-/var/lib/opencuttles/images/default}"
build="${OPENCUTTLES_CVD_BUILD:-aosp-main/aosp_cf_x86_64_phone-userdebug}"
image_url="${OPENCUTTLES_CVD_IMAGE_URL:-}"
host_package_url="${OPENCUTTLES_CVD_HOST_PACKAGE_URL:-}"
keep_archives="${OPENCUTTLES_KEEP_IMAGE_ARCHIVES:-0}"

sudo install -d -m 0755 "$(dirname "$image_dir")"
sudo install -d -m 0755 "$image_dir"
sudo chown -R "${SUDO_USER:-$USER}:${SUDO_USER:-$USER}" "$image_dir"

if [[ -f "${image_dir}/bin/cvd" || -f "${image_dir}/bin/launch_cvd" ]]; then
  echo "Default Cuttlefish image already appears prepared at ${image_dir}."
  exit 0
fi

if command -v cvd >/dev/null 2>&1; then
  echo "Fetching Cuttlefish image with cvd fetch: ${build}"
  if cvd fetch --help 2>&1 | grep -q -- '--target_directory'; then
    cvd fetch --default_build="$build" --target_directory="$image_dir"
  else
    (
      cd "$image_dir"
      cvd fetch --default_build="$build"
    )
  fi
else
  if [[ -z "$image_url" || -z "$host_package_url" ]]; then
    echo "cvd command not found and explicit artifact URLs were not provided." >&2
    echo "Set OPENCUTTLES_CVD_IMAGE_URL and OPENCUTTLES_CVD_HOST_PACKAGE_URL, or install Cuttlefish tools first." >&2
    exit 1
  fi

  work_dir="$(mktemp -d)"
  trap 'rm -rf "$work_dir"' EXIT
  echo "Downloading Cuttlefish image artifact..."
  curl -fL "$image_url" -o "${work_dir}/device-img.zip"
  echo "Downloading Cuttlefish host package artifact..."
  curl -fL "$host_package_url" -o "${work_dir}/cvd-host_package.tar.gz"

  tar -xzf "${work_dir}/cvd-host_package.tar.gz" -C "$image_dir"
  unzip -o "${work_dir}/device-img.zip" -d "$image_dir"
fi

if [[ "$keep_archives" != "1" ]]; then
  find "$image_dir" -maxdepth 1 \( -name '*.zip' -o -name '*.tar.gz' \) -delete
fi

sudo chown -R opencuttles:opencuttles "$image_dir" 2>/dev/null || true
echo "Default Cuttlefish image is ready at ${image_dir}."
