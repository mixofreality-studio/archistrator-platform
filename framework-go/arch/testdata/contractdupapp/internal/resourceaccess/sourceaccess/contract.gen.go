package sourceaccess

// SourceAccess is the generated ResourceAccess port (contract surface).
type SourceAccess interface {
	Read(path string) (Blob, error)
}

// Blob is a generated contract value type.
type Blob struct{ Data []byte }

// NewSourceAccess constructs the access (generated constructor).
func NewSourceAccess() SourceAccess { return &sourceAccess{} }
