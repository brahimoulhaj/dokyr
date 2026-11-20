#!/bin/sh
# End-to-end test of the installer on a fresh machine: installs the panel from a locally
# built image with the rendered bin/install.sh, then checks what a person would find.
#
#   make installer-e2e          (builds the image and the installer, then runs this)
#
# It installs for real: Docker networks, /var/lib/<slug>, ports 80 and 443, a command in
# /usr/local/bin. So it only runs where that is the point: in CI, or with E2E_FRESH_VM=1
# on a machine made for it.
set -eu

SLUG=${1:?usage: e2e.sh SLUG ENV_PREFIX IMAGE}
PREFIX=${2:?}
IMAGE=${3:?}

[ "${CI:-}" = "true" ] || [ "${E2E_FRESH_VM:-}" = "1" ] || {
  echo "refusing to install on this machine: set E2E_FRESH_VM=1 on a throwaway VM" >&2
  exit 2
}
[ -f bin/install.sh ] || { echo "bin/install.sh is missing: run make installer" >&2; exit 2; }

fail() { echo "FAIL: $*" >&2; sudo docker logs --tail 80 "$SLUG" 2>&1 || true; exit 1; }
ok() { echo "ok: $*"; }

run_installer() {
  sudo env "${PREFIX}IMAGE=$IMAGE" "${PREFIX}PULL=never" sh bin/install.sh
}

echo "== fresh install"
OUT=$(run_installer) || { echo "$OUT"; fail "the installer exited with an error"; }
echo "$OUT"
echo "$OUT" | grep -q "is running" || fail "the installer did not say the panel is running"
echo "$OUT" | grep -Eq 'https?://[^ ]+' || fail "no setup link was printed"
ok "installer finished and printed a setup link"

curl -fsS http://127.0.0.1:8080/api/health >/dev/null || fail "the panel does not answer on 127.0.0.1:8080"
ok "panel answers on its recovery port"
curl -fsS http://127.0.0.1:8080/api/meta | grep -q '"setup_required":true' || fail "a fresh install must wait for its owner"
ok "fresh install waits for setup"

# Through the proxy, as a browser reaches it: the server's address on port 80.
i=0
until curl -fsS -o /dev/null http://127.0.0.1/api/health; do
  i=$((i + 1))
  [ "$i" -lt 60 ] || fail "the proxy does not serve the panel on port 80"
  sleep 1
done
ok "proxy serves the panel on port 80"

"$SLUG" version >/dev/null || fail "the host command $SLUG does not work"
ok "host command works: $("$SLUG" version)"
[ -f "/var/lib/$SLUG/compose.yaml" ] || sudo test -f "/var/lib/$SLUG/compose.yaml" || fail "no compose file in the data directory"

# The owner, made with the token the installer gives out.
TOKEN=$(sudo "$SLUG" setup-token | grep -Eo 'token=[A-Za-z0-9_-]+' | head -n1 | cut -d= -f2)
[ -n "$TOKEN" ] || fail "setup-token printed no token"
CODE=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'Origin: http://127.0.0.1:8080' \
  -d "{\"token\":\"$TOKEN\",\"email\":\"owner@example.test\",\"password\":\"e2e-Passw0rd-$$\"}" http://127.0.0.1:8080/api/setup)
case "$CODE" in 2*) ok "owner created with the setup token" ;; *) fail "setup answered $CODE" ;; esac
curl -fsS http://127.0.0.1:8080/api/meta | grep -q '"setup_required":false' || fail "setup did not complete"

echo "== second run (an update in place)"
OUT=$(run_installer) || { echo "$OUT"; fail "a second run of the installer failed"; }
echo "$OUT" | grep -q "Already set up" || fail "a second run did not find the existing installation"
curl -fsS http://127.0.0.1:8080/api/meta | grep -q '"setup_required":false' || fail "the owner did not survive a second run"
ok "second run keeps the installation and its data"

echo "installer end-to-end: passed"
