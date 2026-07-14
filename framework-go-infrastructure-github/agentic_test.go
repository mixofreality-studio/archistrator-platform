package github_test

// Satellite-level regression tests for the agentic.go Contents-API surface —
// GetRepoContentsFile in particular, the manifest fast-path's read primitive.
// These exercise the satellite IN ISOLATION against the testinfra FakeGitHub's
// stateful repo catalog, so the satellite carries its own coverage independent
// of any consuming ResourceAccess.

import (
	"context"
	"testing"

	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

func TestGetRepoContentsFile_Found(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.EnableRepoCatalog()
	fake.SeedRepo("acme", "widget", "", nil, true)
	fake.SeedRepoFile("acme", "widget", ".aiarch/manifest.json", []byte(`{"k":"v"}`))
	c := newClient(t, fake.BaseURL())

	content, found, err := c.GetRepoContentsFile(context.Background(), "acme/widget", ".aiarch/manifest.json", "tok")
	if err != nil {
		t.Fatalf("GetRepoContentsFile: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for a seeded file")
	}
	if string(content) != `{"k":"v"}` {
		t.Fatalf("content = %q, want %q", content, `{"k":"v"}`)
	}
}

func TestGetRepoContentsFile_NotFoundIsNotAnError(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.EnableRepoCatalog()
	fake.SeedRepo("acme", "widget", "", nil, true)
	c := newClient(t, fake.BaseURL())

	content, found, err := c.GetRepoContentsFile(context.Background(), "acme/widget", ".aiarch/manifest.json", "tok")
	if err != nil {
		t.Fatalf("a missing file must not error, got: %v", err)
	}
	if found {
		t.Fatal("expected found=false for an absent file")
	}
	if content != nil {
		t.Fatalf("expected nil content on a miss, got %q", content)
	}
}

func TestGetRepoContentsFile_OtherErrorsPropagate(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/repos/acme/widget/contents/.aiarch/manifest.json", gh.Response{Status: 500, Body: `{"message":"boom"}`})
	c := newClient(t, fake.BaseURL())

	_, _, err := c.GetRepoContentsFile(context.Background(), "acme/widget", ".aiarch/manifest.json", "tok")
	if kindOf(err) != fwra.Transient {
		t.Fatalf("GetRepoContentsFile kind = %v, want Transient (500 mapping)", kindOf(err))
	}
}
