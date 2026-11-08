// Package conformance drives the first-pass-correctness conformance pipeline:
// embedded project.json fixtures pushed through every generator, built, booted
// and probed. This file holds the embedded fixture set.
package conformance

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed all:testdata
var fixtureFS embed.FS

// Fixtures returns the embedded fixture tree rooted at testdata, one directory
// per fixture, each holding a project.json.
func Fixtures() fs.FS {
	sub, err := fs.Sub(fixtureFS, "testdata")
	if err != nil {
		// testdata is embedded at compile time; Sub only fails on an invalid
		// path literal, which is a programming error.
		panic(err)
	}
	return sub
}

// FixtureNames lists the embedded fixtures, sorted.
func FixtureNames() []string {
	entries, err := fs.ReadDir(Fixtures(), ".")
	if err != nil {
		panic(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// Fixture returns the project.json bytes of the named fixture.
func Fixture(name string) ([]byte, error) {
	return fs.ReadFile(Fixtures(), name+"/project.json")
}
