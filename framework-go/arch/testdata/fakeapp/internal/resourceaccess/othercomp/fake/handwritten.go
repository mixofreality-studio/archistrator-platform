package othercompfake

import "example.com/fakeapp/internal/resourceaccess/widgetaccess"

// FakeOtherComp is a HAND-WRITTEN file (no *.gen.go suffix) inside a fake-leaf
// package. The generated-test-double exemption requires EVERY file in the package
// to be generated, so this file's mere presence must keep this package fully
// subject to layer/import/naming enforcement — proving a hand-written package
// cannot dodge the rules just by naming itself "fake". It imports a sibling
// ResourceAccess package, a forbidden sideways import.
type FakeOtherComp struct {
	Widget widgetaccess.WidgetAccess
}
