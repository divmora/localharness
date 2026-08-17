#!/bin/bash
set -e

echo "Building Docker image for E2E tests..."
docker build -t local-harness-e2e -f gui/Dockerfile.e2e .

echo "Running E2E tests in privileged container..."
docker run --rm --privileged -v $(pwd):/app -w /app/gui local-harness-e2e npm run test:e2e:headless
