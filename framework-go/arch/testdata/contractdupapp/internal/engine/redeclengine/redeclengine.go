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

// LocalBlob is a local mirror of the generated sourceaccess.Blob type: same
// field shape, but a DIFFERENT named type declared here, in a different
// package than sourceaccess.
type LocalBlob struct{ Data []byte }

// ForeignMirrorTypeAccess is an EXPORTED near-miss interface that DOES reach
// ifaceMethodSetEqual as a rule-d candidate (exported, cross-package against
// sourceaccess's generated SourceAccess contract): same method NAME and arity
// (Read) as SourceAccess, but its result type is LocalBlob — a local mirror of
// Blob, not the generated type itself. This must NOT fire rule d: it proves
// types.Identical (structural signature equality), not just name-set/count,
// gates the cross-package match too — if ifaceMethodSetEqual were regressed
// to a name/count-only compare, this interface would incorrectly start
// firing rule d against sourceaccess.SourceAccess.
type ForeignMirrorTypeAccess interface {
	Read(path string) (LocalBlob, error)
}
