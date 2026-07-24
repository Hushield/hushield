#!/usr/bin/env bash
# scripts/deploy.sh -- builds (and tags) the spamfilter-server image, then
# hands off to a host-specific push/deploy step.
#
# The deploy target is deliberately NOT decided yet (see Task 14 brief); this
# script only builds the image locally and prints where to plug in the real
# target. It is safe to run as-is: with no DEPLOY_TARGET configured it just
# builds and exits 0.
#
# Usage:
#   IMAGE=spamfilter-server TAG=latest ./scripts/deploy.sh
#
# Env vars:
#   IMAGE          Image name to build/tag (default: spamfilter-server)
#   TAG            Image tag (default: latest)
#   DEPLOY_TARGET  Optional; when set, selects a push/deploy path below
#                  (e.g. "registry", "ssh", "paas"). Unset by default.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

IMAGE="${IMAGE:-spamfilter-server}"
TAG="${TAG:-latest}"
DEPLOY_TARGET="${DEPLOY_TARGET:-}"

echo "Building ${IMAGE}:${TAG} ..."
docker build -t "${IMAGE}:${TAG}" .
echo "Built ${IMAGE}:${TAG}."

# --- TODO: choose a host --------------------------------------------------
# The actual push/deploy destination is intentionally not wired up yet.
# Set DEPLOY_TARGET and fill in one of the blocks below when a host is
# chosen. Until then this section is a guarded no-op: it explains what to
# do instead of failing.
if [ -z "$DEPLOY_TARGET" ]; then
  cat <<EOF

No DEPLOY_TARGET configured -- skipping push/deploy.
Image is available locally as ${IMAGE}:${TAG}.

To deploy, set DEPLOY_TARGET and implement the matching block in this
script, e.g.:

  # DEPLOY_TARGET=registry
  #   docker tag "${IMAGE}:${TAG}" registry.example.com/spamfilter/server:${TAG}
  #   docker push registry.example.com/spamfilter/server:${TAG}

  # DEPLOY_TARGET=ssh
  #   docker save "${IMAGE}:${TAG}" | ssh deploy@host.example.com \\
  #     'docker load && docker compose -f /opt/spamfilter/docker-compose.yml up -d server'

  # DEPLOY_TARGET=paas
  #   flyctl deploy --image "${IMAGE}:${TAG}"
  #   # or: railway up / render deploy / etc.

EOF
  exit 0
fi

case "$DEPLOY_TARGET" in
  registry|ssh|paas)
    echo "DEPLOY_TARGET=${DEPLOY_TARGET} is not implemented yet." >&2
    echo "Fill in the matching block above once a host is chosen." >&2
    exit 1
    ;;
  *)
    echo "Unknown DEPLOY_TARGET '${DEPLOY_TARGET}'. Expected one of: registry, ssh, paas." >&2
    exit 1
    ;;
esac
