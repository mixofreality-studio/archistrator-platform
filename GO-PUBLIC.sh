#!/usr/bin/env bash
# Make archistrator-platform public + publish the TS lib to public npmjs.org.
# Run this yourself (visibility change exposes code; npm publish needs YOUR npmjs login).
set -euo pipefail
cd "$(dirname "$0")"

echo "==> 1. push the npmjs-config commit"
git push origin HEAD:main

echo "==> 2. make the repo PUBLIC (Go modules become zero-auth go get)"
gh repo edit davidmarne/archistrator-platform --visibility public --accept-visibility-change-consequences

echo "==> 3. publish @davidmarne/archistrator-platform-framework-web to public npmjs.org"
cd framework-web
# remove any GitHub-Packages .npmrc so this publishes to the default registry (npmjs.org)
rm -f .npmrc
if ! npm whoami --registry=https://registry.npmjs.org/ >/dev/null 2>&1; then
  echo "   You're not logged into npmjs.org. Run:  npm login   (then re-run this script)"
  exit 1
fi
npm publish --access public   # publishConfig.access=public is set; scope @davidmarne must be yours on npmjs
echo "==> DONE: platform public + @davidmarne/archistrator-platform-framework-web on npmjs.org"
echo "    NOTE: if the npm scope @davidmarne isn't yours on npmjs.org, publish will 403 —"
echo "    create it (it's free; tied to your npm username) or tell me to rename the scope."
