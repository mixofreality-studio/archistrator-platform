#!/usr/bin/env bash
# Publish archistrator-platform v0.1.0 — run this yourself (creates the private GitHub
# repo, pushes, tags the 7 Go modules, publishes the npm package). Idempotent-ish.
set -euo pipefail
cd "$(dirname "$0")"

echo "==> create private repo + push (skips if it already exists)"
if ! gh repo view davidmarne/archistrator-platform >/dev/null 2>&1; then
  gh repo create davidmarne/archistrator-platform --private --source=. --remote=origin --push
else
  git remote get-url origin >/dev/null 2>&1 || git remote add origin git@github.com:davidmarne/archistrator-platform.git
  git push -u origin HEAD:main
fi

echo "==> tag the 7 Go modules at v0.1.0 (framework-go + 6 satellites)"
git tag -f framework-go/v0.1.0
for s in github keycloak llm otel postgres temporal; do
  git tag -f framework-go-infrastructure-$s/v0.1.0
done
git push -f origin --tags

echo "==> publish the npm package to GitHub Packages"
cd framework-web
printf '@davidmarne:registry=https://npm.pkg.github.com\n//npm.pkg.github.com/:_authToken=%s\n' "$(gh auth token)" > .npmrc
npm publish
rm -f .npmrc

echo "==> DONE: archistrator-platform v0.1.0 published (Go tags + @davidmarne/archistrator-platform-web@0.1.0)"
