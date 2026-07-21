#!/usr/bin/env bash
#
# Tests for apply-caddyfile.sh.
#
# This runs the real script unprivileged against a temp directory by setting
# OPENCUTTLES_SUDO="" — the point is to exercise the actual sed expressions,
# since the bug being fixed here was a silent HTTPS-to-plaintext downgrade that
# no amount of reading the diff would have caught.
#
# Usage: bash scripts/ubuntu/apply-caddyfile.test.sh
set -uo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
subject="${script_dir}/apply-caddyfile.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0
check() {
  local name="$1" want="$2" got="$3"
  if [[ "$want" == "$got" ]]; then
    printf 'pass: %s\n' "$name"
  else
    printf 'FAIL: %s\n      want: %q\n      got:  %q\n' "$name" "$want" "$got"
    failures=$((failures + 1))
  fi
}

# The packaged template, as it ships: plain HTTP on every host.
template="${work}/Caddyfile"
cat > "$template" <<'EOF'
:80 {
	encode gzip zstd
	reverse_proxy 127.0.0.1:8080
}
EOF

# apply asserts the exit status too: install.sh and upgrade.sh both run under
# "set -e", so a non-zero exit here aborts a deploy midway even when the file
# was written correctly.
apply() {
  if ! OPENCUTTLES_SUDO="" bash "$subject" "$template" "$1" >/dev/null; then
    printf 'FAIL: apply exited non-zero for %s\n' "$1"
    failures=$((failures + 1))
  fi
}

# --- A fresh install has nothing to preserve -------------------------------
fresh="${work}/fresh.caddy"
apply "$fresh"
check "fresh install uses the template" ":80 {" "$(head -1 "$fresh")"

# --- An HTTPS appliance keeps its site address -----------------------------
# This is the regression: upgrade.sh copied the template over this file, so a
# host configured for HTTPS silently went back to plaintext on every upgrade —
# and desktop runners refuse plaintext, so every runner would drop.
https="${work}/https.caddy"
cat > "$https" <<'EOF'
https://testral.example {
	encode gzip zstd
	reverse_proxy 127.0.0.1:8080
}
EOF
apply "$https"
check "HTTPS site address survives an upgrade" "https://testral.example {" "$(head -1 "$https")"
check "template body is still applied" "1" "$(grep -c 'reverse_proxy 127.0.0.1:8080' "$https")"

# --- A self-signed appliance keeps its tls directive too -------------------
selfsigned="${work}/selfsigned.caddy"
cat > "$selfsigned" <<'EOF'
https://10.1.0.104 {
    tls /etc/opencuttles/tls/appliance.crt /etc/opencuttles/tls/appliance.key
	encode gzip zstd
	reverse_proxy 127.0.0.1:8080
}
EOF
apply "$selfsigned"
check "self-signed site address survives" "https://10.1.0.104 {" "$(head -1 "$selfsigned")"
check "tls directive survives" "1" \
  "$(grep -c '^[[:space:]]*tls /etc/opencuttles/tls/appliance.crt /etc/opencuttles/tls/appliance.key$' "$selfsigned")"

# --- Re-running must not stack duplicate tls lines -------------------------
# Caddy rejects a duplicated tls directive, so an upgrade that appended one each
# time would take the dashboard down on the second upgrade rather than the first.
apply "$selfsigned"
apply "$selfsigned"
check "tls directive is not duplicated" "1" \
  "$(grep -c '^[[:space:]]*tls /etc/opencuttles/tls/' "$selfsigned")"
check "site address still right after repeats" "https://10.1.0.104 {" "$(head -1 "$selfsigned")"

# --- A plain-HTTP host stays plain HTTP ------------------------------------
http="${work}/http.caddy"
cat > "$http" <<'EOF'
:80 {
	reverse_proxy 127.0.0.1:8080
}
EOF
apply "$http"
check "plain HTTP is left alone" ":80 {" "$(head -1 "$http")"
check "no tls line is invented" "0" "$(grep -c 'tls /etc/opencuttles/tls/' "$http")"

# --- A missing template is an error, not a silent no-op --------------------
if OPENCUTTLES_SUDO="" bash "$subject" "${work}/nope" "${work}/out.caddy" >/dev/null 2>&1; then
  printf 'FAIL: a missing template was accepted\n'
  failures=$((failures + 1))
else
  printf 'pass: a missing template is rejected\n'
fi

echo
if [[ "$failures" -gt 0 ]]; then
  echo "${failures} failure(s)"
  exit 1
fi
echo "all checks passed"
