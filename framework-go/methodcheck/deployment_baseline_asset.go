package methodcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// deployment_baseline_asset.go RENDERS the web-app deployment prototype into the
// method asset the drafting agent reads.
//
// The asset is a generated output, not an authored document. Hand-writing it
// would create a second definition of the front door — one the gate does not
// know about — and the two would drift the first time either changed. The
// generator (cmd/gen-deployment-prototype) writes it into the method-assets
// module; a drift test regenerates and compares, exactly as every other
// generated output in this codebase is kept honest.

// PrototypeAssetPath is the repo-relative path of the generated asset, from the
// archistrator-platform root.
const PrototypeAssetPath = "method-assets/assets/claude/method-assets/webapp-deployment-prototype.md"

// prototypeFragment is the JSON shape the asset embeds: the baseline environment
// alongside the containers it instances, so the agent can lift both halves.
type prototypeFragment struct {
	Containers  []DeployContainer     `json:"containers"`
	Environment DeploymentEnvironment `json:"environment"`
}

// RenderWebAppPrototypeAsset renders the method asset from WebAppBaseline. The
// prose explains what must survive adaptation; the JSON fence is the fragment
// the agent starts from.
func RenderWebAppPrototypeAsset() ([]byte, error) {
	fragment, err := json.MarshalIndent(prototypeFragment{
		Containers:  WebAppBaselineContainers(),
		Environment: WebAppBaseline(),
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("methodcheck: render prototype asset: %w", err)
	}

	var b bytes.Buffer
	b.WriteString(prototypeAssetProse)
	b.WriteString("```json\n")
	b.Write(fragment)
	b.WriteString("\n```\n")
	return b.Bytes(), nil
}

// prototypeAssetProse is the asset's frontmatter and prose, everything above
// the JSON fence. It says what must survive the agent's adaptation; the rules it
// names are the ones that judge the result.
const prototypeAssetProse = `---
name: webapp-deployment-prototype
kind: doctrine-asset
description: The deployment topology every web application built here starts from — a person on their device, the frontend surface they use, and the platform's standard authenticated front door in front of the application. READ-ONLY and GENERATED from methodcheck.WebAppBaseline(); referenced (not re-derived) by [[the-method-operational-concepts]] and enforced by DEP-EDGE-GATEWAY.
---

# Web-app deployment prototype

**This file is generated.** It is rendered from ` + "`methodcheck.WebAppBaseline()`" + ` in
framework-go and rewritten by ` + "`cmd/gen-deployment-prototype`" + `. Edit the Go value, not this
file — a hand-edit is overwritten and, worse, would make the template disagree with the gate that
judges its output.

## Why this exists

Platform runtime doctrine #1 already fixes the shape of the front door: external callers reach
in-process Clients through an edge gateway, which authenticates the request against the identity
provider before forwarding it. Every web application built here has the same one.

Because that shape existed only as prose, every drafting agent re-derived it, and most of them
dropped it — the gateway ended up fronting nothing, the identity provider ended up filed under
"external services" with no edge to anything, and the deployment view came out as a set of
disconnected boxes with no frontend at all.

**Start from this fragment. Do not re-derive it.**

## What must survive your adaptation

Rename the nodes for the project at hand, add its own containers, infrastructure and external
systems, and set the instance counts. Four things must still be true when you are done, because
` + "`DEP-EDGE-GATEWAY`" + ` and ` + "`DEP-FRONTEND-PRESENT`" + ` check them:

1. **A frontend surface is deployed** — a container whose ` + "`surface`" + ` is ` + "`spa`" + `,
   ` + "`mobile`" + ` or ` + "`cli`" + `, or an element with the ` + "`agentHarness`" + ` role.
   Every system is reached by someone; a view of only back-end services has omitted how it is used.
2. **The frontend reaches the gateway**, the **gateway authenticates against the identity
   provider**, and the **gateway forwards to the application container**. A gateway that
   authenticates nothing, or that nothing reaches, is not the platform's front door.
3. **Every element carries a ` + "`key`" + `** unique within its environment, so relationships can
   address it.
4. **Every container instance, infrastructure node, external system and person has at least one
   edge.** An element nothing reaches and that reaches nothing is not deployed, it is decoration.

## What you do NOT author

The application edges — the SPA calling the server, the server reading its database — are
**derived** from the committed System relationships and must not be restated here. Author only the
edges whose endpoints are not System components at all: the person, the browser, the gateway and
the identity provider.

## Where the chain does NOT apply

` + "`DEP-EDGE-GATEWAY`" + ` is gated on the presence of a gateway element. A local dev-boot
topology that binds a loopback port has neither gateway nor identity provider, and must not
pretend otherwise — model the real local story instead.

## The fragment

`
