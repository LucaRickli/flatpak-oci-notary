// Package glob implements the simple wildcard patterns used throughout the
// notary: "*" matches any sequence of characters (including "/"), "?" matches
// exactly one character and everything else matches literally. An empty
// pattern matches everything.
package glob

// Match reports whether s matches pattern.
func Match(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	// Iterative wildcard matching with single-star backtracking.
	p, i := 0, 0
	star, mark := -1, 0
	for i < len(s) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[i]):
			p++
			i++
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, i
			p++
		case star >= 0:
			p = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// MatchAny reports whether s matches any of the patterns. An empty pattern
// list matches everything.
func MatchAny(patterns []string, s string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if Match(p, s) {
			return true
		}
	}
	return false
}
