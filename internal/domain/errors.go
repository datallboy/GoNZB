package domain

import "errors"

// ErrProviderBusy indicates all nntp connections are in use
var ErrProviderBusy = errors.New("all providers busy")

// ErrArticleNotFound indicates a 430 response from Usenet
var ErrArticleNotFound = errors.New("article not found")
