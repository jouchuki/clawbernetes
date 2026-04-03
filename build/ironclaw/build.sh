#!/usr/bin/env bash
# Build the IronClaw container image.
#
# Usage:
#   ./build/ironclaw/build.sh [IMAGE_TAG] [CONTAINER_TOOL]
#
# Examples:
#   ./build/ironclaw/build.sh clawbernetes/ironclaw:latest docker
#   ./build/ironclaw/build.sh clawbernetes/ironclaw:v0.1.0 podman

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

IMAGE="${1:-clawbernetes/ironclaw:latest}"
CONTAINER_TOOL="${2:-docker}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

# TODO: Replace with the real IronClaw repository when available.
# echo "==> Cloning IronClaw..."
# git clone https://github.com/example/ironclaw.git "$TMPDIR/ironclaw"
# echo "==> Building base image: ${IMAGE}-base"
# "$CONTAINER_TOOL" build -t "${IMAGE}-base" "$TMPDIR/ironclaw"

echo "==> Building final image: ${IMAGE}"
"$CONTAINER_TOOL" build \
    --build-arg "BASE_IMAGE=${IMAGE}-base" \
    -t "$IMAGE" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$SCRIPT_DIR"

echo "==> Done: ${IMAGE}"
