package collect

// SanitizeDisplay strips C0/C1 controls and DEL so process-derived
// paths and titles cannot inject terminal escape sequences.
func SanitizeDisplay(s string) string {
	if s == "" {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
