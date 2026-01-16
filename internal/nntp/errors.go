package nntp

import "errors"

// ErrProviderBusy indicates all nntp connections are in use
var ErrProviderBusy = errors.New("all providers busy")
