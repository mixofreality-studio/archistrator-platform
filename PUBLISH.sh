#!/usr/bin/env bash
# Re-publish archistrator-platform CLEAN — force-pushes the de-bloated history (no
# node_modules), re-tags the 7 Go modules, republishes the npm package under its new
# name (@davidmarne/archistrator-platform-framework-web). Run this yourself.
# The old @davidmarne/archistrator-platform-web@0.1.0 is orphaned — delete it later in
# the GitHub Packages UI if you like (nothing consumes it).
set -euo pipefail
cd "$(dirname "$0")"

echo "==> force-push the cleaned branch (drops the 56k node_modules from history)"
git remote get-url origin >/dev/null 2>&1 || git remote add origin git@github.com:davidmarne/archistrator-platform.git
git push -f origin HEAD:main

echo "==> re-tag + force-push the 7 Go module tags at the clean commit"
git tag -f framework-go/v0.1.0
for s in github keycloak llm otel postgres temporal; do git tag -f framework-go-infrastructure-$s/v0.1.0; done
git push -f origin --tags

echo "==> publish @davidmarne/archistrator-platform-framework-web@0.1.0"
cd framework-web
printf '@davidmarne:registry=https://npm.pkg.github.com\n//npm.pkg.github.com/:_authToken=%s\n' "$(gh auth token)" > .npmrc
npm publish
rm -f .npmrc

echo "==> DONE: archistrator-platform clean + @davidmarne/archistrator-platform-framework-web@0.1.0 published"
