#!/usr/bin/env bash
# Build the Hermes container image.
#
# Usage:
#   ./build/hermes/build.sh [IMAGE_TAG] [CONTAINER_TOOL]
#
# Examples:
#   ./build/hermes/build.sh clawbernetes/hermes:latest docker
#   ./build/hermes/build.sh clawbernetes/hermes:v0.1.0 podman

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

IMAGE="${1:-clawbernetes/hermes:latest}"
CONTAINER_TOOL="${2:-docker}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

# TODO: Replace with the real Hermes repository when available.
# echo "==> Cloning Hermes..."
# git clone https://github.com/example/hermes-agent.git "$TMPDIR/hermes"
# echo "==> Building base image: ${IMAGE}-base"
# "$CONTAINER_TOOL" build -t "${IMAGE}-base" "$TMPDIR/hermes"

echo "==> Building final image: ${IMAGE}"
"$CONTAINER_TOOL" build \
    --build-arg "BASE_IMAGE=${IMAGE}-base" \
    -t "$IMAGE" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$SCRIPT_DIR"

echo "==> Done: ${IMAGE}"
