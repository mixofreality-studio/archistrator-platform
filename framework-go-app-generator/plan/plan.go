// Package plan is the manifest vocabulary of the app generator: the paths a generator writes and
// who owns each one after it has run. Edit scope, drift and admission read these outputs, so
// what a generator writes and what the platform believes it writes come from one source.
package plan

import (
	"slices"
	"sort"
)

// OutputRole is who owns a planned path after the generator runs.
//
//sumtype:decl
type OutputRole interface{ isOutputRole() }

// Generated is rewritten on every run; the committed bytes must equal regeneration.
type Generated struct{}

// Seam is written once when absent (cmd/server/hooks.go, the webgen bootstrap seats); the app owns it after.
type Seam struct{}

func (Generated) isOutputRole() {}
func (Seam) isOutputRole()      {}

// Output is one path a consumer writes, repo-relative and slash-separated.
type Output struct {
	Path string
	Role OutputRole
}

// File is an Output with its content.
type File struct {
	Output
	Content []byte
}

// RoleName is "generated" or "seam". The switch has no default (gochecksumtype keeps it exhaustive).
func RoleName(r OutputRole) string {
	switch r.(type) {
	case Generated:
		return "generated"
	case Seam:
		return "seam"
	}
	panic("unreachable: sealed OutputRole")
}

// IsSeam reports whether r is a Seam. The switch has no default (gochecksumtype keeps it exhaustive),
// so a new role cannot fall silently into the branch of the old two.
func IsSeam(r OutputRole) bool {
	switch r.(type) {
	case Generated:
		return false
	case Seam:
		return true
	}
	panic("unreachable: sealed OutputRole")
}

// Paths returns the sorted paths of outs whose role is r's type.
func Paths(outs []Output, r OutputRole) []string {
	want := RoleName(r)
	var out []string
	for _, o := range outs {
		if RoleName(o.Role) == want {
			out = append(out, o.Path)
		}
	}
	slices.Sort(out)
	return out
}

// Outputs is the manifest of files: their paths and roles without the content, sorted by path.
func Outputs(files []File) []Output {
	out := make([]Output, 0, len(files))
	for _, f := range files {
		out = append(out, f.Output)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Sort orders files by path, in place.
func Sort(files []File) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}
