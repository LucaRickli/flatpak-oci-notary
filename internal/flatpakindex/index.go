// Package flatpakindex builds the OCI registry index flatpak fetches for
// oci+http(s):// remotes (GET <remote>/index/static?...), and serves it.
//
// The format mirrors flatpak's parser (common/flatpak-json-oci.c): a single
// "Registry" base URL and per-repository image lists whose "Labels" carry the
// flatpak metadata.
package flatpakindex

import (
	"cmp"
	"net/url"
	"slices"
	"strings"

	"github.com/lucarickli/flatpak-oci-notary/internal/glob"
	"github.com/lucarickli/flatpak-oci-notary/internal/store"
)

// Response is the top-level index document. All fields are mandatory for
// flatpak; slices must never be nil (marshalled as null).
type Response struct {
	Registry string       `json:"Registry"`
	Results  []Repository `json:"Results"`
}

type Repository struct {
	Name   string  `json:"Name"`
	Images []Image `json:"Images"`
	Lists  []List  `json:"Lists"`
}

type Image struct {
	Tags         []string          `json:"Tags"`
	Digest       string            `json:"Digest"`
	MediaType    string            `json:"MediaType"`
	OS           string            `json:"OS"`
	Architecture string            `json:"Architecture"`
	Annotations  map[string]string `json:"Annotations"`
	Labels       map[string]string `json:"Labels"`
}

// List is a manifest list entry. The notary flattens image indexes into
// per-platform images, so it only exists to keep the schema complete.
type List struct {
	Digest    string   `json:"Digest"`
	MediaType string   `json:"MediaType"`
	Images    []Image  `json:"Images"`
	Tags      []string `json:"Tags"`
}

// Filter is the parsed query of an index request.
type Filter struct {
	Architectures []string
	OS            []string
	Tags          []string
	// LabelExists lists label keys that must be present.
	LabelExists []string
	// LabelEquals maps label keys to required values.
	LabelEquals map[string]string
}

// ParseFilter parses the query flatpak (and other clients of the registry
// index protocol) send, e.g.
// "label:org.flatpak.ref:exists=1&architecture=amd64&os=linux&tag=latest".
func ParseFilter(q url.Values) Filter {
	f := Filter{LabelEquals: map[string]string{}}
	for key, values := range q {
		switch {
		case key == "architecture":
			f.Architectures = append(f.Architectures, nonEmpty(values)...)
		case key == "os":
			f.OS = append(f.OS, nonEmpty(values)...)
		case key == "tag":
			f.Tags = append(f.Tags, nonEmpty(values)...)
		case strings.HasPrefix(key, "label:"):
			label := strings.TrimPrefix(key, "label:")
			if l, ok := strings.CutSuffix(label, ":exists"); ok {
				if len(values) > 0 && values[0] != "0" && values[0] != "false" {
					f.LabelExists = append(f.LabelExists, l)
				}
			} else if len(values) > 0 {
				f.LabelEquals[label] = values[0]
			}
		}
	}
	slices.Sort(f.Architectures)
	slices.Sort(f.OS)
	slices.Sort(f.Tags)
	slices.Sort(f.LabelExists)
	return f
}

// CacheKey is a canonical representation of the filter.
func (f Filter) CacheKey() string {
	var b strings.Builder
	b.WriteString("a=" + strings.Join(f.Architectures, ","))
	b.WriteString("&o=" + strings.Join(f.OS, ","))
	b.WriteString("&t=" + strings.Join(f.Tags, ","))
	b.WriteString("&le=" + strings.Join(f.LabelExists, ","))
	keys := make([]string, 0, len(f.LabelEquals))
	for k := range f.LabelEquals {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		b.WriteString("&l:" + url.QueryEscape(k) + "=" + url.QueryEscape(f.LabelEquals[k]))
	}
	return b.String()
}

func (f Filter) matches(img *store.Image) bool {
	if len(f.Architectures) > 0 && !slices.Contains(f.Architectures, img.Architecture) {
		return false
	}
	if len(f.OS) > 0 && !slices.Contains(f.OS, img.OS) {
		return false
	}
	if len(f.Tags) > 0 && !slices.ContainsFunc(img.Tags, func(t string) bool { return slices.Contains(f.Tags, t) }) {
		return false
	}
	for _, l := range f.LabelExists {
		if _, ok := img.Labels[l]; !ok && !(l == "org.flatpak.ref" && img.Ref != "") {
			return false
		}
	}
	for k, v := range f.LabelEquals {
		if img.Labels[k] != v {
			return false
		}
	}
	return true
}

// MatchSources reports whether an image is selected by a repository's sources:
// at least one include rule matches and no exclude rule does.
func MatchSources(sources []store.Source, img *store.Image) bool {
	included := false
	for _, s := range sources {
		if !sourceMatches(s, img) {
			continue
		}
		if s.Exclude {
			return false
		}
		included = true
	}
	return included
}

func sourceMatches(s store.Source, img *store.Image) bool {
	if !glob.Match(s.RepositoryPattern, img.Repository) || !glob.Match(s.RefPattern, img.Ref) {
		return false
	}
	if s.TagPattern == "" || s.TagPattern == "*" {
		return true
	}
	return slices.ContainsFunc(img.Tags, func(t string) bool { return glob.Match(s.TagPattern, t) })
}

// Select applies the repository sources and the request filter, then keeps a
// single image per flatpak ref (flatpak's summary can only hold one commit
// per ref): the newest by creation time, then by indexing time.
func Select(sources []store.Source, f Filter, images []*store.Image) []*store.Image {
	best := map[string]*store.Image{}
	for _, img := range images {
		if !MatchSources(sources, img) || !f.matches(img) {
			continue
		}
		if cur, ok := best[img.Ref]; !ok || newer(img, cur) {
			best[img.Ref] = img
		}
	}
	out := make([]*store.Image, 0, len(best))
	for _, img := range best {
		out = append(out, img)
	}
	slices.SortFunc(out, func(a, b *store.Image) int {
		return cmp.Or(cmp.Compare(a.Ref, b.Ref), cmp.Compare(a.Repository, b.Repository))
	})
	return out
}

func newer(a, b *store.Image) bool {
	at, bt := createdOrZero(a), createdOrZero(b)
	if at != bt {
		return at > bt
	}
	if !a.IndexedAt.Equal(b.IndexedAt) {
		return a.IndexedAt.After(b.IndexedAt)
	}
	return a.ID > b.ID
}

func createdOrZero(i *store.Image) int64 {
	if i.Created == nil {
		return 0
	}
	return i.Created.UnixNano()
}

// Build assembles the index document for the selected images.
func Build(registryURL string, images []*store.Image) *Response {
	resp := &Response{Registry: strings.TrimRight(registryURL, "/") + "/", Results: []Repository{}}
	byRepo := map[string]int{}
	for _, img := range images {
		i, ok := byRepo[img.Repository]
		if !ok {
			i = len(resp.Results)
			byRepo[img.Repository] = i
			resp.Results = append(resp.Results, Repository{Name: img.Repository, Images: []Image{}, Lists: []List{}})
		}
		labels := img.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		tags := img.Tags
		if tags == nil {
			tags = []string{}
		}
		resp.Results[i].Images = append(resp.Results[i].Images, Image{
			Tags:         tags,
			Digest:       img.Digest,
			MediaType:    img.MediaType,
			OS:           img.OS,
			Architecture: img.Architecture,
			Annotations:  map[string]string{},
			Labels:       labels,
		})
	}
	slices.SortFunc(resp.Results, func(a, b Repository) int { return cmp.Compare(a.Name, b.Name) })
	return resp
}

func nonEmpty(v []string) []string {
	out := v[:0:0]
	for _, s := range v {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
