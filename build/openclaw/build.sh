#!/usr/bin/env bash
# Build the OpenClaw container image with Clawbernetes plugins.
#
# Usage:
#   ./build/openclaw/build.sh [IMAGE_TAG] [CONTAINER_TOOL]
#
# Examples:
#   ./build/openclaw/build.sh clawbernetes/openclaw:latest docker
#   ./build/openclaw/build.sh clawbernetes/openclaw:v0.1.0 podman

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

IMAGE="${1:-clawbernetes/openclaw:latest}"
CONTAINER_TOOL="${2:-docker}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "==> Cloning OpenClaw..."
git clone https://github.com/orq-ai/openclaw.git "$TMPDIR/openclaw"
cd "$TMPDIR/openclaw" && git submodule update --init --recursive 2>/dev/null || true

echo "==> Building base image: ${IMAGE}-base"
"$CONTAINER_TOOL" build -t "${IMAGE}-base" "$TMPDIR/openclaw"

echo "==> Building final image with plugins: ${IMAGE}"
"$CONTAINER_TOOL" build \
    --build-arg "BASE_IMAGE=${IMAGE}-base" \
    -t "$IMAGE" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$SCRIPT_DIR"

echo "==> Done: ${IMAGE}"
