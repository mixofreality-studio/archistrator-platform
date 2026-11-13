package testgen

import (
	"fmt"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// emit_acceptance_http.go is the REST dialect of the acceptance emitters: a REST client's step is a
// request to its generated route, in a Go test through a scenariohost.RESTClient session and in a
// Playwright spec through contractCall. Both are spelled from the route planner's table
// (managerRoutes), so a test cannot name a route the handler does not mount.

// restRoutesVar is the name of a REST client's route table in the acceptance file: one table per
// client, because an acceptance file reaches several.
func restRoutesVar(client string) string { return "clientRoutes" + exportName(ident(client)) }

// restSessionLine opens a session on the launched app as who, over the client's routes.
func restSessionLine(s accSession, who string) string {
	return fmt.Sprintf("%s := scenariohost.RESTClient(t, app, %s, %s)", s.variable(), who, restRoutesVar(s.client))
}

// acceptanceRouteTable is the route table of one REST client of the acceptance file.
func acceptanceRouteTable(key string, sc methodcheck.ServiceContract, routes map[string]restRoute) (string, error) {
	return restRouteTableNamed(key, sc, routes, restRoutesVar(key))
}

// restRouteOf is the route a REST client's operation requests, by the client's op name
// (ManagerBase+Op, the key managerRoutes plans under).
func restRouteOf(routes map[string]restRoute, client string, op methodcheck.ContractOperation) (restRoute, error) {
	r, ok := routes[op.Name]
	if !ok {
		return restRoute{}, fmt.Errorf("testgen: acceptance: %s operation %s is no Manager's generated route", client, op.Name)
	}
	return r, nil
}
