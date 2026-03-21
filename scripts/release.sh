#!/bin/bash
# Release a new version of the sea CLI.
# Usage: ./scripts/release.sh <version>
#
# Runs tests, commits, tags, pushes, and watches the release CI.
set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <version>"
  echo "Example: $0 v2.1.0"
  exit 1
fi

VERSION="$1"

echo "=== Running tests ==="
go test ./... -count=1
echo ""

echo "=== Building locally ==="
go build -o bin/sea ./cmd/sea/
echo ""

echo "=== Current status ==="
git status --short
echo ""

read -p "Commit, tag $VERSION, and push? [y/N] " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Aborted."
  exit 1
fi

# Commit if there are changes
if [ -n "$(git status --porcelain)" ]; then
  git add -A
  git commit -m "Release $VERSION"
fi

git tag "$VERSION"
git push origin main "$VERSION"

echo ""
echo "=== Watching release CI ==="
sleep 5
RUN_ID=$(gh run list --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status

echo ""
echo "=== Released ==="
gh release view "$VERSION" --json tagName,assets --jq '{tag: .tagName, assets: [.assets[].name]}'
