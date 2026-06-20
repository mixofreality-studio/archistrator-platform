package github_test

// Satellite-level regression tests for the GitHub-App client. These exercise the
// satellite IN ISOLATION (App-JWT mint, the REST calls, error classification)
// against the testinfra FakeGitHub, so the satellite carries its own coverage
// independent of any consuming ResourceAccess.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	fwgithub "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

func newClient(t *testing.T, baseURL string) *fwgithub.AppClient {
	t.Helper()
	keyPEM, err := gh.GenerateAppKeyPEM()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	c, err := fwgithub.NewAppClient("42", keyPEM, baseURL)
	if err != nil {
		t.Fatalf("NewAppClient: %v", err)
	}
	return c
}

func kindOf(err error) fwra.Kind {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return fwra.Unknown
}

func TestNewAppClientRejectsBadKey(t *testing.T) {
	if _, err := fwgithub.NewAppClient("1", "not a pem", ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("bad key kind = %v, want ContractMisuse", kindOf(err))
	}
	if _, err := fwgithub.NewAppClient("", "x", ""); kindOf(err) != fwra.ContractMisuse {
		t.Fatalf("empty appID kind = %v, want ContractMisuse", kindOf(err))
	}
}

func TestFindInstallationAndToken(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/app/installations", gh.JSON(200, []map[string]any{
		{"id": 99, "account": map[string]any{"login": "acme"}},
	}))
	fake.On("POST", "/app/installations/99/access_tokens", gh.JSON(201, map[string]any{
		"token": "ghs_x", "expires_at": time.Now().Add(time.Hour).UTC(),
	}))
	c := newClient(t, fake.BaseURL())

	id, err := c.FindInstallation(context.Background(), "acme")
	if err != nil || id != 99 {
		t.Fatalf("FindInstallation = %d, %v; want 99, nil", id, err)
	}
	tok, exp, err := c.MintInstallationToken(context.Background(), 99)
	if err != nil || tok != "ghs_x" || exp.IsZero() {
		t.Fatalf("MintInstallationToken = %q, %v, %v", tok, exp, err)
	}
	// the discovery used an App-JWT bearer
	req := fake.Requests()[0]
	if !strings.HasPrefix(req.Auth, "Bearer ") {
		t.Fatalf("discovery auth = %q, want App-JWT Bearer", req.Auth)
	}
}

func TestClassifyStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		want   fwra.Kind
	}{
		{401, fwra.Auth},
		{403, fwra.Auth},
		{404, fwra.NotFound},
		{405, fwra.Conflict},
		{409, fwra.Conflict},
		{422, fwra.ContractMisuse},
		{429, fwra.Transient},
		{503, fwra.Transient},
		{418, fwra.Infrastructure},
	}
	for _, tc := range cases {
		if got := kindOf(fwgithub.ClassifyStatus(tc.status, "op")); got != tc.want {
			t.Errorf("ClassifyStatus(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestCreateOrgRepoAlreadyExists(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("POST", "/orgs/acme/repos", gh.Response{Status: 422, Body: `{"message":"exists"}`})
	fake.On("GET", "/repos/acme/aiarch-p1", gh.JSON(200, map[string]any{"full_name": "acme/aiarch-p1"}))
	c := newClient(t, fake.BaseURL())

	full, existed, err := c.CreateOrgRepo(context.Background(), "acme", "aiarch-p1", "ghs_x", true, fwgithub.CreateRepoOptions{})
	if err != nil || !existed || full != "acme/aiarch-p1" {
		t.Fatalf("CreateOrgRepo already-exists = %q, %v, %v", full, existed, err)
	}
}

// TestCreateOrgRepoWithMetadataAndList proves the framework's create-with-metadata +
// installation-repo enumeration over the STATEFUL fake: a repo created with a
// description + topic appears in the very next ListInstallationRepos carrying both
// (faithful read-after-write — the property the discover-by-enumeration catalog
// relies on).
func TestCreateOrgRepoWithMetadataAndList(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.EnableRepoCatalog()
	c := newClient(t, fake.BaseURL())

	full, existed, err := c.CreateOrgRepo(context.Background(), "acme", "aiarch-p1", "ghs_x", true,
		fwgithub.CreateRepoOptions{Description: "My Project", Topics: []string{"aiarch-project"}})
	if err != nil || existed || full != "acme/aiarch-p1" {
		t.Fatalf("CreateOrgRepo = %q, existed=%v, err=%v", full, existed, err)
	}

	repos, err := c.ListInstallationRepos(context.Background(), "ghs_x")
	if err != nil {
		t.Fatalf("ListInstallationRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("ListInstallationRepos returned %d repos, want 1: %+v", len(repos), repos)
	}
	got := repos[0]
	if got.FullName != "acme/aiarch-p1" || got.Description != "My Project" {
		t.Fatalf("listed repo = %+v, want acme/aiarch-p1 / 'My Project'", got)
	}
	hasTopic := false
	for _, tp := range got.Topics {
		if tp == "aiarch-project" {
			hasTopic = true
		}
	}
	if !hasTopic {
		t.Fatalf("listed repo missing aiarch-project topic: %+v", got.Topics)
	}
}

func TestMintInstallationTokenAuth(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("POST", "/app/installations/99/access_tokens", gh.Response{Status: 403, Body: `{"message":"revoked"}`})
	c := newClient(t, fake.BaseURL())
	if _, _, err := c.MintInstallationToken(context.Background(), 99); kindOf(err) != fwra.Auth {
		t.Fatalf("token mint 403 kind = %v, want Auth", kindOf(err))
	}
}
