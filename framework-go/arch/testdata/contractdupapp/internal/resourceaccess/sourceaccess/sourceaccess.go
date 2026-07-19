package sourceaccess

type sourceAccess struct{}

func (s *sourceAccess) Read(path string) (Blob, error) { return Blob{}, nil }
