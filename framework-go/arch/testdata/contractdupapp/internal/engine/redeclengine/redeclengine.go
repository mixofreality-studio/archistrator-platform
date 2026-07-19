package redeclengine

import "example.com/contractdupapp/internal/resourceaccess/sourceaccess"

// ForeignSourceAccess is a ROGUE exported hand-written interface declared in a
// DIFFERENT package than the one that owns the generated contract it
// duplicates: its method set (names + signatures, using the generated
// sourceaccess.Blob type) is a full, exact match of sourceaccess.SourceAccess
// — rule d (no-foreign-contract-redecl) must fire on it. Consumers should
// import and use sourceaccess.SourceAccess directly instead.
type ForeignSourceAccess interface {
	Read(path string) (sourceaccess.Blob, error)
}
