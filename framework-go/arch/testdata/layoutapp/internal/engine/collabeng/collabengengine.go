// Package collabeng is a CLEAN Engine-layer fixture that other components'
// hooks files use as a real collaborator (DCT §12 B2). Its public package may
// be imported by a hooks file; its internal/ sub-package and its generated
// fake may not.
package collabeng

import "example.com/layoutapp/internal/engine/collabeng/internal/store"

// Engine is the public constructor surface a hooks file may build.
type Engine struct{}

// Price delegates to the component's internal store.
func (e *Engine) Price(n int) (int, error) { return store.Lookup(n), nil }
