// Package flatpakmeta reads the flatpak metadata keyfile
// (label org.flatpak.metadata, see flatpak-metadata(5)) and derives the
// values the notary stores per image.
package flatpakmeta

import (
	"slices"
	"strings"
)

// KeyFile is a parsed GLib key file: groups of key/value pairs.
type KeyFile struct {
	groups map[string]map[string]string
	// order lists the groups in the order they first appear.
	order []string
}

// ParseKeyFile parses the subset of the GLib key file format flatpak
// metadata uses, following GLib's rules: leading whitespace on a line is
// skipped; a line is a comment when '#' is then first; blank lines are
// ignored; "[group]" starts a group (the name runs to the last ']');
// "key=value" keeps the key without trailing whitespace and the value
// without leading whitespace; a later duplicate key replaces the earlier
// one; "\s", "\n", "\t", "\r" and "\\" in values are unescaped. Localized
// keys ("key[locale]=") are ignored. Where GLib would fail (a key before
// any group, a line without '='), the line is skipped instead, so one odd
// line never hides the rest of the file.
func ParseKeyFile(data string) *KeyFile {
	kf := &KeyFile{groups: map[string]map[string]string{}}
	var current map[string]string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimLeft(line, " \t\f\v")
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			end := strings.LastIndexByte(line, ']')
			if end < 1 {
				continue
			}
			name := line[1:end]
			if name == "" || strings.ContainsAny(name, "[]") {
				continue
			}
			current = kf.groups[name]
			if current == nil {
				current = map[string]string{}
				kf.groups[name] = current
				kf.order = append(kf.order, name)
			}
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimRight(key, " \t")
		if key == "" || strings.HasSuffix(key, "]") {
			continue // empty or localized key
		}
		current[key] = unescape(strings.TrimLeft(value, " \t"))
	}
	return kf
}

// Groups lists the group names in order of appearance.
func (kf *KeyFile) Groups() []string { return append([]string(nil), kf.order...) }

// HasGroup reports whether the group exists.
func (kf *KeyFile) HasGroup(group string) bool {
	_, ok := kf.groups[group]
	return ok
}

// Get returns the value of key in group and whether it is set.
func (kf *KeyFile) Get(group, key string) (string, bool) {
	v, ok := kf.groups[group][key]
	return v, ok
}

// Keys lists the keys of a group, sorted.
func (kf *KeyFile) Keys(group string) []string {
	g := kf.groups[group]
	out := make([]string, 0, len(g))
	for k := range g {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func unescape(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c != '\\' || i+1 >= len(v) {
			b.WriteByte(c)
			continue
		}
		switch v[i+1] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			// GLib rejects unknown escapes; keep them literally.
			b.WriteByte(c)
			continue
		}
		i++
	}
	return b.String()
}
