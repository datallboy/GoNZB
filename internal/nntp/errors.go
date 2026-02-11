package nntp

import "errors"

// ErrProviderBusy indicates all nntp connections are in use
var ErrProviderBusy = errors.New("all providers busy")
var ErrArticleNotFound = errors.New("article not found (430)")
