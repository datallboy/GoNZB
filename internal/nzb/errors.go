package nzb

import "errors"

// ErrArticleNotFound indicates a 430 response from Usenet
var ErrArticleNotFound = errors.New("article not found")
