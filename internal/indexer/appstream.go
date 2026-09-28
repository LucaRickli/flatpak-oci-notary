package indexer

import (
	"encoding/xml"
	"io"
	"strings"
)

type appstreamText struct {
	Attrs []xml.Attr `xml:",any,attr"`
	Text  string     `xml:",chardata"`
}

func (t appstreamText) lang() string {
	for _, a := range t.Attrs {
		if a.Name.Local == "lang" {
			return a.Value
		}
	}
	return ""
}

type appstreamComponent struct {
	Names     []appstreamText `xml:"name"`
	Summaries []appstreamText `xml:"summary"`
	Releases  struct {
		Release []struct {
			Version string `xml:"version,attr"`
		} `xml:"release"`
	} `xml:"releases"`
}

type appstreamInfo struct {
	Name, Summary, Version string
}

// parseAppstream extracts the untranslated name, summary and latest release
// version from the org.freedesktop.appstream.appdata label (an appstream
// <component>, possibly wrapped in <components>).
func parseAppstream(data string) appstreamInfo {
	if data == "" {
		return appstreamInfo{}
	}
	dec := xml.NewDecoder(strings.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF || err != nil {
			return appstreamInfo{}
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "component" {
			continue
		}
		var c appstreamComponent
		if err := dec.DecodeElement(&c, &start); err != nil {
			return appstreamInfo{}
		}
		info := appstreamInfo{Name: untranslated(c.Names), Summary: untranslated(c.Summaries)}
		if len(c.Releases.Release) > 0 {
			info.Version = c.Releases.Release[0].Version
		}
		return info
	}
}

func untranslated(texts []appstreamText) string {
	for _, t := range texts {
		if t.lang() == "" {
			return strings.TrimSpace(t.Text)
		}
	}
	if len(texts) > 0 {
		return strings.TrimSpace(texts[0].Text)
	}
	return ""
}
