#!/usr/bin/env bash
#
# deploy.sh -- ship the HuShield server and recompute binaries to the deploy
# host and restart the services.
#
# Invoke via the Makefile, which builds first and enforces the test gate:
#
#   make deploy-server    # gated on the Go suite only (backend hotfix)
#   make deploy           # gated on the full suite (Go + iOS)
#
# Both run 'build-linux-arm64', which cross-compiles static binaries and
# asserts they are ARM aarch64 -- the deploy host is a Graviton instance, and a
# stray amd64 build would only reveal itself as "exec format error" on the box.
#
# Environment:
#   DEPLOY_HOST   target host or IP           (default 10.30.1.244)
#   DEPLOY_USER   ssh user                    (default ec2-user)
#   SSH_KEY       identity file               (default ~/.ssh/pm-prod.pem)
#   SKIP_RESTART  set to 1 to upload only, without restarting the service
#
# The binaries are self-contained: CGO_ENABLED=0 and migrations are go:embed'ed,
# so nothing else needs shipping and migrations apply on service start.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

DEPLOY_HOST="${DEPLOY_HOST:-10.30.1.244}"
DEPLOY_USER="${DEPLOY_USER:-ec2-user}"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/pm-prod.pem}"
SKIP_RESTART="${SKIP_RESTART:-0}"

REMOTE_DIR=/opt/hushield/bin
SSH_OPTS=(-i "$SSH_KEY" -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new)

die() { echo "ERROR: $*" >&2; exit 1; }

for f in bin/hushield-server bin/hushield-recompute; do
	[ -f "$f" ] || die "$f not found. Run 'make build-linux-arm64' first, or use 'make deploy-server'."
	file "$f" | grep -q 'ARM aarch64' \
		|| die "$f is not ARM aarch64. The deploy host is Graviton; this binary would not execute."
done

echo "==> Deploying to ${DEPLOY_USER}@${DEPLOY_HOST}"

echo "==> Uploading binaries"
scp "${SSH_OPTS[@]}" bin/hushield-server bin/hushield-recompute \
	"${DEPLOY_USER}@${DEPLOY_HOST}:/tmp/" >/dev/null

echo "==> Installing and restarting"
ssh "${SSH_OPTS[@]}" "${DEPLOY_USER}@${DEPLOY_HOST}" "sudo bash -s" <<REMOTE
set -euo pipefail

install -o hushield -g hushield -m 0755 /tmp/hushield-server    ${REMOTE_DIR}/hushield-server
install -o hushield -g hushield -m 0755 /tmp/hushield-recompute ${REMOTE_DIR}/hushield-recompute
rm -f /tmp/hushield-server /tmp/hushield-recompute

if [ "${SKIP_RESTART}" = "1" ]; then
	echo "    SKIP_RESTART=1, not restarting"
	exit 0
fi

systemctl restart hushield-api

# Give the service time to load config, connect to MySQL, and run migrations.
# config.Load() hard-fails on a bad production config, so a crash-loop here is
# almost always configuration -- the journal names the offending variable.
sleep 4
if ! systemctl is-active --quiet hushield-api; then
	echo "    hushield-api failed to start. Recent log:" >&2
	journalctl -u hushield-api -n 30 --no-pager >&2
	exit 1
fi

echo "    hushield-api active"
REMOTE

echo "==> Health check"
if ssh "${SSH_OPTS[@]}" "${DEPLOY_USER}@${DEPLOY_HOST}" \
	'curl -fsS --max-time 10 http://127.0.0.1:8080/healthz >/dev/null'; then
	echo "    /healthz OK"
else
	die "/healthz did not return success. Check: journalctl -u hushield-api -n 50"
fi

echo "==> Deploy complete"
