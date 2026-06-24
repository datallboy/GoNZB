package nntp

import (
	"errors"
	"fmt"
	"strings"
)

// ErrProviderBusy indicates all nntp connections are in use
var ErrProviderBusy = errors.New("all providers busy")
var ErrArticleNotFound = errors.New("article not found (430)")

type ArticleNotFoundError struct {
	MessageID string
	Attempts  []string
}

func (e *ArticleNotFoundError) Error() string {
	if e == nil {
		return ErrArticleNotFound.Error()
	}
	attempts := make([]string, 0, len(e.Attempts))
	for _, attempt := range e.Attempts {
		if strings.TrimSpace(attempt) != "" {
			attempts = append(attempts, strings.TrimSpace(attempt))
		}
	}
	if len(attempts) == 0 {
		return ErrArticleNotFound.Error()
	}
	if strings.TrimSpace(e.MessageID) != "" {
		return fmt.Sprintf("%s providers=%s message_id=%s", ErrArticleNotFound.Error(), strings.Join(attempts, ","), strings.TrimSpace(e.MessageID))
	}
	return fmt.Sprintf("%s providers=%s", ErrArticleNotFound.Error(), strings.Join(attempts, ","))
}

func (e *ArticleNotFoundError) Is(target error) bool {
	return target == ErrArticleNotFound
}
