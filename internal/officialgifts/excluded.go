package officialgifts

import "strings"

// Durov personal gifts are omitted from automatic local catalog imports.
var durovGiftIDs = map[int64]struct{}{
	5915521180483191380: {}, // Durov's Cap
	5834651202612102354: {}, // Durov's Glasses
	6003477390536213997: {}, // Durov's Figurine
	6001229799790478558: {}, // Durov's Boots
	6001425315291727333: {}, // Durov's Coat
}

// ExcludedFromImport reports whether an official gift should be skipped during
// automatic catalog bootstrap (Durov personal gifts and name matches).
func ExcludedFromImport(id int64, title string) bool {
	if _, ok := durovGiftIDs[id]; ok {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(title)), "durov")
}
