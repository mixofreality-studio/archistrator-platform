package methodcheck

import (
	"strings"
	"testing"
)

// TP-CLIENT-CONTRACTED: every Client component has a serviceContracts entry.

func TestTP_ClientContracted_Fires(t *testing.T) {
	_, _, p := clientDesign(t)
	msg := ruleMessages(clientContractFindings(p), ruleTPClientContracted)
	for _, id := range []string{"web-app", "web-api", "mcp-server"} {
		if !strings.Contains(msg, `"`+id+`"`) {
			t.Fatalf("client %s has no contract and must be named:\n%s", id, msg)
		}
	}
	if strings.Contains(msg, "list-manager") {
		t.Fatalf("a Manager is not a Client:\n%s", msg)
	}
}

func TestTP_ClientContracted_Passes(t *testing.T) {
	_, _, p := clientDesign(t)
	for _, key := range []string{"webApp", "webApi"} {
		p.ServiceContracts[key] = ServiceContract{Component: key}
	}
	// a facet filed under another key still contracts the component it names.
	p.ServiceContracts["mcpServerTools"] = ServiceContract{Component: "mcpServer"}
	if msg := ruleMessages(clientContractFindings(p), ruleTPClientContracted); msg != "" {
		t.Fatalf("every Client now has a contract:\n%s", msg)
	}
}
