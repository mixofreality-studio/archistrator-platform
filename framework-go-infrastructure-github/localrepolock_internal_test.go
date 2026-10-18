package github

import "testing"

// TestLocalRepoPath — only a file:// URL or a filesystem path is a LOCAL target
// (locked); https, ssh and scp-style remotes are not.
func TestLocalRepoPath(t *testing.T) {
	for url, local := range map[string]bool{
		"file:///tmp/state":                    true,
		"/tmp/state":                           true,
		"relative/state":                       true,
		"https://github.com/o/r.git":           false,
		"ssh://git@github.com/o/r.git":         false,
		"git@github.com:o/r.git":               false,
		"http://localhost:3000/gitea/repo.git": false,
	} {
		if _, got := localRepoPath(url); got != local {
			t.Errorf("localRepoPath(%q) local=%v, want %v", url, got, local)
		}
	}
}
