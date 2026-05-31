#!/usr/bin/env bash
set -euo pipefail

# Build and run the Dockerized end-to-end tests from the repo root.
cd "$(dirname "$0")/.."
docker build -f e2e/Dockerfile -t wolf-the-cleaner-e2e .
docker run --rm wolf-the-cleaner-e2e
