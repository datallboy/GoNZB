package nzb

import "github.com/datallboy/gonzb/internal/categories/newsnab"

// GetCategoryName maps Newsnab IDs to human-readable strings.
func GetCategoryName(id string) string {
	if parsed, ok := newsnab.ParseID(id); ok {
		return newsnab.DisplayName(parsed)
	}
	return newsnab.DisplayName(newsnab.OtherMisc)
}
