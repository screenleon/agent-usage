package filter

// Wants reports whether name is selected. An empty list means all names.
func Wants(names []string, name string) bool {
	if len(names) == 0 {
		return true
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
