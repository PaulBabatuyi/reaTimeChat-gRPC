package normalize

import "strings"

// Email returns a normalized form of an email address suitable for
// storage and comparisons. Normalization currently trims surrounding
// whitespace and lower-cases the address.
func Email(e string) string {
    return strings.ToLower(strings.TrimSpace(e))
}
