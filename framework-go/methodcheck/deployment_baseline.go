package methodcheck

// deployment_baseline.go owns the WEB-APP DEPLOYMENT PROTOTYPE — the front door
// every system built on this platform shares.
//
// Platform runtime doctrine #1 already fixes the shape: external callers reach
// in-process Clients through an edge gateway, which authenticates the request
// against the identity provider before forwarding it. Because that shape existed
// only as prose, every drafting agent re-derived it from scratch and most of them
// dropped it — the gateway ended up fronting nothing and the identity provider
// ended up filed under "external services" with no edge to anything.
//
// So it lives here, typed, with two consumers and no second definition: the
// method asset the drafting agent starts from is MATERIALIZED from this value,
// and DEP-EDGE-GATEWAY judges against the same roles it declares. A template the
// gate did not know about would drift from the gate the moment either changed.

// BaselineKeys are the element keys the prototype uses. They are exported so the
// asset materializer and its tests can name the same elements the baseline does
// without restating string literals.
const (
	BaselineKeyPerson       = "person-user"
	BaselineKeyDevice       = "node-client-device"
	BaselineKeyBrowser      = "node-web-browser"
	BaselineKeySPAInstance  = "ci-spa-browser"
	BaselineKeyHarness      = "ext-agent-harness"
	BaselineKeyGateway      = "infra-edge-gateway"
	BaselineKeyIdP          = "infra-identity-provider"
	BaselineKeyAppNode      = "node-app-server"
	BaselineKeyAppInstance  = "ci-app-server"
	BaselineContainerSPAKey = "spa"
	BaselineContainerAppKey = "app-server"
)

// WebAppBaseline returns the canonical starting topology for a web application:
// a person on a client device whose browser runs the single-page application,
// an agent harness as the second supported frontend surface, and the standard
// authenticated front door in front of the application container.
//
// It is a STARTING POINT, not a finished model. The drafting agent renames the
// nodes for the project at hand, adds the project's own containers,
// infrastructure and external systems, and keeps the front door intact. What it
// must not do is re-derive the front door — that is the duplication this value
// exists to end.
func WebAppBaseline() DeploymentEnvironment {
	return DeploymentEnvironment{
		Profile: profileCloud,
		Title:   "Cloud",
		Persons: []DeploymentPerson{{
			Key:         BaselineKeyPerson,
			Name:        "User",
			Description: "Uses the system through its web or agent surface.",
		}},
		Nodes: []DeploymentNode{
			{
				Key:        BaselineKeyDevice,
				Name:       "User's computer",
				Technology: "Windows / macOS / Linux",
				Instances:  1,
				Children: []DeploymentNode{{
					Key:        BaselineKeyBrowser,
					Name:       "Web browser",
					Technology: "Chrome, Firefox, Safari or Edge",
					Instances:  1,
					ContainerInstances: []ContainerInstance{{
						Key:          BaselineKeySPAInstance,
						ContainerKey: BaselineContainerSPAKey,
						Note:         "the single-page application, executing in the user's browser",
					}},
				}},
				SoftwareSystemInstances: []SoftwareSystemInstance{{
					Key:         BaselineKeyHarness,
					Name:        "Agent harness",
					Technology:  "MCP client",
					Description: "Reaches the same API through the system's generated tool surface.",
					Role:        roleAgentHarness,
				}},
			},
			{
				Key:        BaselineKeyAppNode,
				Name:       "Application cluster",
				Technology: "Kubernetes",
				InfrastructureNodes: []InfrastructureNode{
					{
						Key:         BaselineKeyGateway,
						Name:        "Edge gateway",
						Technology:  "Envoy Gateway / Gateway API",
						Description: "TLS termination and OIDC authentication at the edge.",
						Role:        roleGateway,
					},
					{
						Key:         BaselineKeyIdP,
						Name:        "Identity provider",
						Technology:  "Keycloak",
						Description: "Issues and validates the tokens the gateway checks.",
						Role:        roleIdentityProvider,
					},
				},
				ContainerInstances: []ContainerInstance{{
					Key:          BaselineKeyAppInstance,
					ContainerKey: BaselineContainerAppKey,
					Note:         "the application server",
				}},
			},
		},
		Relationships: []DeploymentRelationship{
			{
				From: BaselineKeySPAInstance, To: BaselineKeyGateway,
				Label: "Makes API calls to", Technology: "JSON/HTTPS", Mode: modeSync,
			},
			{
				From: BaselineKeyHarness, To: BaselineKeyGateway,
				Label: "Makes tool calls to", Technology: "MCP/HTTPS", Mode: modeSync,
			},
			{
				From: BaselineKeyGateway, To: BaselineKeyIdP,
				Label: "Authenticates the request against", Technology: "OIDC", Mode: modeSync,
			},
			{
				From: BaselineKeyGateway, To: BaselineKeyAppInstance,
				Label: "Forwards the authenticated request to", Technology: "HTTP", Mode: modeSync,
			},
		},
	}
}

// WebAppBaselineContainers returns the two containers the baseline instances:
// the single-page application the browser executes, and the application server
// behind the gateway.
func WebAppBaselineContainers() []DeployContainer {
	return []DeployContainer{
		{
			Key:         BaselineContainerSPAKey,
			Name:        "Single-page application",
			Technology:  "TypeScript / React",
			Description: "Provides the system's functionality to users via their web browser.",
			Surface:     surfaceSPA,
		},
		{
			Key:         BaselineContainerAppKey,
			Name:        "Application server",
			Technology:  "Go",
			Description: "Serves the API and hosts the Clients, Managers, Engines and ResourceAccess.",
			Surface:     surfaceService,
		},
	}
}
