package github_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	fwgithub "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"
	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// TestCreateTree_Guards: bad input is rejected as ContractMisuse before any wire
// call. An entry with a blank path or sha counts as bad input even when the
// entries around it are fine.
func TestCreateTree_Guards(t *testing.T) {
	fake, c := gitTreesFixture(t)
	pre := len(fake.Requests())
	good := fwgithub.TreeWriteEntry{Path: "a.md", SHA: "sha-a"}
	cases := map[string]struct {
		repo    string
		entries []fwgithub.TreeWriteEntry
	}{
		"empty repo":  {"", []fwgithub.TreeWriteEntry{good}},
		"blank repo":  {"   ", []fwgithub.TreeWriteEntry{good}},
		"no entries":  {"acme/widget", nil},
		"blank path":  {"acme/widget", []fwgithub.TreeWriteEntry{good, {Path: " ", SHA: "sha-b"}}},
		"blank sha":   {"acme/widget", []fwgithub.TreeWriteEntry{good, {Path: "b.md", SHA: ""}}},
		"both blank":  {"acme/widget", []fwgithub.TreeWriteEntry{{}}},
		"later blank": {"acme/widget", []fwgithub.TreeWriteEntry{good, good, {Path: "c.md", SHA: "\t"}}},
	}
	for name, tc := range cases {
		if _, err := c.CreateTree(context.Background(), tc.repo, "", tc.entries, "tok"); kindOf(err) != fwra.ContractMisuse {
			t.Errorf("%s: kind = %v, want ContractMisuse (err: %v)", name, kindOf(err), err)
		}
	}
	if len(fake.Requests()) != pre {
		t.Fatalf("guards must fire before any wire call; requests went %d → %d", pre, len(fake.Requests()))
	}
}

// TestCreateTree_WirePayload: every entry goes out as a regular-file blob item,
// in the caller's order, and base_tree is sent only when the caller supplies
// one. An unborn branch omits it, so the tree carries only the entries.
func TestCreateTree_WirePayload(t *testing.T) {
	fake, c := gitTreesFixture(t)
	fake.On("POST", "/repos/acme/widget/git/trees", gh.Response{Status: 201, Body: `{"sha":"tree-1"}`})
	entries := []fwgithub.TreeWriteEntry{{Path: "z.md", SHA: "sha-z"}, {Path: "a/b.md", SHA: "sha-b"}}

	for _, base := range []string{"", "base-1"} {
		sha, err := c.CreateTree(context.Background(), "acme/widget", base, entries, "tok")
		if err != nil {
			t.Fatalf("CreateTree(base=%q): %v", base, err)
		}
		if sha != "tree-1" {
			t.Fatalf("CreateTree(base=%q) sha = %q, want tree-1", base, sha)
		}
	}

	var bodies []map[string]any
	for _, r := range fake.Requests() {
		if r.Method == "POST" && strings.HasSuffix(r.Path, "/git/trees") {
			var body map[string]any
			if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
				t.Fatalf("decode tree body %q: %v", r.Body, err)
			}
			bodies = append(bodies, body)
		}
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 tree POSTs, got %d", len(bodies))
	}
	wantTree := []any{
		map[string]any{"path": "z.md", "mode": "100644", "type": "blob", "sha": "sha-z"},
		map[string]any{"path": "a/b.md", "mode": "100644", "type": "blob", "sha": "sha-b"},
	}
	for i, body := range bodies {
		if !reflect.DeepEqual(body["tree"], wantTree) {
			t.Errorf("POST %d tree = %v, want %v", i, body["tree"], wantTree)
		}
	}
	if _, has := bodies[0]["base_tree"]; has {
		t.Errorf("an empty base tree must be omitted, got body %v", bodies[0])
	}
	if got := bodies[1]["base_tree"]; got != "base-1" {
		t.Errorf("base_tree = %v, want base-1", got)
	}
}
