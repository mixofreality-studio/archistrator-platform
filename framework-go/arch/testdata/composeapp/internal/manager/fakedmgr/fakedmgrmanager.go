package fakedmgr

type impl struct{}

func (impl) Do() error { return nil }

// NewFakedManager builds the manager.
func NewFakedManager() FakedManager { return impl{} }
