package flatpakmeta

import (
	"strings"
	"unicode/utf8"
)

// Label is the image label carrying the metadata keyfile.
const Label = "org.flatpak.metadata"

// Metadata holds the values the notary derives from a metadata keyfile.
type Metadata struct {
	// Runtime is the runtime the flatpak builds on, as "<id>/<arch>/<branch>"
	// (the runtime= key of [Application] or [Runtime], falling back to
	// [ExtensionOf] like flatpak does); empty if none.
	Runtime string
	// ExtensionOf is the full ref an extension extends ([ExtensionOf] ref);
	// empty otherwise.
	ExtensionOf string
	// HasExtraData reports whether installing downloads extra data from
	// external URLs: an [Extra Data] group with a uri/uriN or name/nameN key.
	HasExtraData bool
}

// maxValueLength bounds stored values (flatpak limits ids to 255 bytes).
const maxValueLength = 1024

// Parse derives Metadata from the keyfile text. Values that are not
// well-formed refs are dropped rather than stored.
func Parse(data string) Metadata {
	if data == "" {
		return Metadata{}
	}
	kf := ParseKeyFile(data)
	var m Metadata
	for _, group := range []string{"Application", "Runtime", "ExtensionOf"} {
		if v, ok := kf.Get(group, "runtime"); ok && validPref(v) {
			m.Runtime = v
			break
		}
	}
	if v, ok := kf.Get("ExtensionOf", "ref"); ok && validRef(v) {
		m.ExtensionOf = v
	}
	for _, key := range kf.Keys("Extra Data") {
		v, _ := kf.Get("Extra Data", key)
		if v != "" && extraDataKey(key) {
			m.HasExtraData = true
			break
		}
	}
	return m
}

// extraDataKey reports whether key is uri, name or one of them with a
// numeric suffix (uri1, name2, ...), as written by flatpak build-finish.
func extraDataKey(key string) bool {
	var suffix string
	switch {
	case strings.HasPrefix(key, "uri"):
		suffix = key[len("uri"):]
	case strings.HasPrefix(key, "name"):
		suffix = key[len("name"):]
	default:
		return false
	}
	for i := 0; i < len(suffix); i++ {
		if suffix[i] < '0' || suffix[i] > '9' {
			return false
		}
	}
	return true
}

// storable reports whether a value fits a text column on every dialect and
// can be a ref at all (refs never contain whitespace).
func storable(v string) bool {
	return v != "" && len(v) <= maxValueLength && utf8.ValidString(v) && !strings.ContainsAny(v, "\x00 \t")
}

// validPref reports whether v is a "<id>/<arch>/<branch>" runtime ref.
func validPref(v string) bool {
	if !storable(v) {
		return false
	}
	parts := strings.Split(v, "/")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

// validRef reports whether v is a full "app|runtime/<id>/<arch>/<branch>" ref.
func validRef(v string) bool {
	if !storable(v) {
		return false
	}
	kind, rest, ok := strings.Cut(v, "/")
	return ok && (kind == "app" || kind == "runtime") && validPref(rest)
}
