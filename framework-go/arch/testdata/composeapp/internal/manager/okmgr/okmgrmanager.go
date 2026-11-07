package okmgr

type impl struct{}

func (impl) Do() error { return nil }

// NewOkManager builds the manager.
func NewOkManager() OkManager { return impl{} }
