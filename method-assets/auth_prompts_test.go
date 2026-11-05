package methodassets

import (
	"strings"
	"testing"
)

// Who is acting is the platform's, never the design's (FIX-055). Three deliveries in a row
// (RUN-017, 018, 019) designed a bespoke identity provider, a proof-in-body protocol and a
// sign-in route, because nothing in the prompts said the framework edge already authenticates
// the caller and hands it to every Manager op as rc.Principal. No structural rule can tell an
// invented ActorProof from honest payload (the architect ruled every candidate evadable or
// falsely positive), so the prompts are the tool, and a rewrite that drops a clause would pass
// every other test.
func TestAuthenticationIsThePlatforms(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	const (
		doctrine       = ".claude/method-assets/platform-runtime-doctrine.md"
		architecture   = ".claude/skills/the-method-architecture/SKILL.md"
		volatility     = ".claude/skills/the-method-volatility-identification/SKILL.md"
		requirements   = ".claude/skills/the-method-requirements-analysis/SKILL.md"
		systemDraft    = ".claude/commands/system-draft.md"
		systemCritique = ".claude/commands/system-critique.md"
		detailedDesign = ".claude/commands/service-detailed-design.md"
		integration    = ".claude/commands/service-integration.md"
	)
	// Every named prompt carries the rule itself, not just a link to it: a draft job reads
	// its own command and skill, and may never open the doctrine.
	for _, path := range []string{doctrine, architecture, volatility, requirements, systemDraft, systemCritique, detailedDesign, integration} {
		if !strings.Contains(string(files[path]), "The platform owns who is acting") {
			t.Errorf("%s no longer says %q", path, "The platform owns who is acting")
		}
	}
	for _, tc := range []struct{ path, want string }{
		// (a) the edge authenticates; the caller reaches every Manager op; the PDP authorizes.
		{doctrine, "rc.Principal"},
		{doctrine, "<PREFIX>_DEV_PRINCIPAL"},
		{doctrine, "PolicyDecisionPoint"},
		// (b) nothing in the System authenticates or identifies the caller.
		{doctrine, "Never draw an identity provider as a Resource"},
		{doctrine, "no field that authenticates or identifies the caller"},
		{doctrine, "never a project volatility"},
		// The deployment-vs-system distinction: the prototype names an identity provider.
		{doctrine, "never in `.systemDesign`"},
		// The carve-out, so the rule does not read as a universal ban.
		{doctrine, "the customer's business"},
		// (c) ownership stays in the Manager, keyed on the subject.
		{doctrine, "rc.Principal.Subject"},
		// (d) the local profile has no sign-in step.
		{doctrine, "no sign-in step"},
		{architecture, "Designed authentication"},
		{volatility, "never a project volatility"},
		{requirements, "\"users must sign in\" is a solution masquerading as a requirement"},
		{systemDraft, "Never draw an identity provider as a Resource"},
		{systemCritique, "REVISE if the draft designs who is acting"},
		{detailedDesign, "no field that authenticates or identifies the caller"},
		{integration, "rc.Principal.Subject"},
	} {
		if !strings.Contains(string(files[tc.path]), tc.want) {
			t.Errorf("%s no longer says %q", tc.path, tc.want)
		}
	}
	// No prompt may teach the pattern the rule forbids: a proof-taking operation as an
	// example label, an identity provider as an ExternalHTTP resource, or authentication as a
	// volatility to encapsulate.
	for path, body := range files {
		for _, stale := range []string{
			"RecognizeActor(proof)",
			"(an identity provider, a payment API)",
			"Authentication scheme must be swappable per deployment",
			`"the system authenticates users" is the behavior`,
		} {
			if strings.Contains(string(body), stale) {
				t.Errorf("%s still says %q", path, stale)
			}
		}
	}
}
