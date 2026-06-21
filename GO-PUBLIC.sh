#!/usr/bin/env bash
# Publish archistrator-platform under the mixofreality-studio GitHub org, PUBLIC,
# and publish the TS lib to public npmjs.org.
#
# Run this YOURSELF: it creates a public repo (exposes code), force-pushes the
# re-tagged Go modules, and `npm publish` needs your npmjs.org login.
#
# Prereqs:
#   - gh authed as a mixofreality-studio org owner (gh auth status)
#   - `npm login` to registry.npmjs.org
#   - the @mixofreality-studio npm org must EXIST on npmjs.org (free; create at
#     npmjs.com/org/create) and your npm user must be a member, or publish 403s.
set -euo pipefail
cd "$(dirname "$0")"

ORG=mixofreality-studio
REPO=archistrator-platform

echo "==> 1. create (if needed) + push the repo PUBLIC under the org"
if ! gh repo view "$ORG/$REPO" >/dev/null 2>&1; then
  # Creates the repo PUBLIC, sets origin, and pushes the current branch as default.
  gh repo create "$ORG/$REPO" --public --source=. --remote=origin --push
else
  git remote set-url origin "git@github.com:$ORG/$REPO.git" 2>/dev/null \
    || git remote add origin "git@github.com:$ORG/$REPO.git"
  git push -u origin HEAD:main
fi
# Repo is created PUBLIC up front, so no visibility-change step is needed.

echo "==> 2. force-push the 7 re-tagged Go module tags (tags moved with the rename)"
git tag -f framework-go/v0.1.0
for s in github keycloak llm otel postgres temporal; do
  git tag -f framework-go-infrastructure-$s/v0.1.0
done
git push -f origin \
  framework-go/v0.1.0 \
  framework-go-infrastructure-github/v0.1.0 \
  framework-go-infrastructure-keycloak/v0.1.0 \
  framework-go-infrastructure-llm/v0.1.0 \
  framework-go-infrastructure-otel/v0.1.0 \
  framework-go-infrastructure-postgres/v0.1.0 \
  framework-go-infrastructure-temporal/v0.1.0

echo "==> 3. publish @$ORG/archistrator-platform-framework-web to public npmjs.org"
cd framework-web
# remove any GitHub-Packages .npmrc so this publishes to the default registry (npmjs.org)
rm -f .npmrc
if ! npm whoami --registry=https://registry.npmjs.org/ >/dev/null 2>&1; then
  echo "   You're not logged into npmjs.org. Run:  npm login   (then re-run this script)"
  exit 1
fi
npm publish --access public   # publishConfig.access=public is set in package.json

echo "==> DONE: $ORG/$REPO is PUBLIC + @$ORG/archistrator-platform-framework-web on npmjs.org"
echo "    NOTE: if the @$ORG scope isn't yours on npmjs.org, publish will 403 —"
echo "    create the npm org at npmjs.com/org/create (free) and join it."
