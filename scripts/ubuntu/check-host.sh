#!/usr/bin/env bash
set -euo pipefail

failures=0

check() {
  local name="$1"
  shift
  if "$@"; then
    printf '[ok]   %s\n' "$name"
  else
    printf '[fail] %s\n' "$name"
    failures=$((failures + 1))
  fi
}

has_command() {
  command -v "$1" >/dev/null 2>&1
}

has_kvm() {
  [[ -e /dev/kvm && -r /dev/kvm && -w /dev/kvm ]]
}

has_nested_virt_hint() {
  grep -E -q '(vmx|svm)' /proc/cpuinfo
}

has_memory() {
  local mem_kb
  mem_kb=$(awk '/MemTotal/ { print $2 }' /proc/meminfo)
  [[ "${mem_kb:-0}" -ge 8000000 ]]
}

has_disk() {
  local available_kb
  available_kb=$(df -Pk /var/lib 2>/dev/null | awk 'NR == 2 { print $4 }')
  [[ "${available_kb:-0}" -ge 50000000 ]]
}

echo "OpenCuttles Ubuntu host readiness"
echo

check "cvd command found" has_command cvd
check "launch_cvd command found" has_command launch_cvd
check "stop_cvd command found" has_command stop_cvd
check "adb command found" has_command adb
check "/dev/kvm is accessible" has_kvm
check "CPU exposes virtualization flags" has_nested_virt_hint
check "host has at least 8 GB RAM" has_memory
check "/var/lib has at least 50 GB free" has_disk

echo
if [[ "$failures" -gt 0 ]]; then
  echo "$failures readiness check(s) failed."
  exit 1
fi

echo "Host is ready for the OpenCuttles MVP baseline."
