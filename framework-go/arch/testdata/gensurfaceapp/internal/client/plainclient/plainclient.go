package plainclient

// PlainThing is exported in a package with NO *.gen.go file, so the gate does not
// target this package at all — hand-written non-component code is left alone.
type PlainThing struct{ Name string }

// PlainFunc is likewise untargeted.
func PlainFunc() {}
