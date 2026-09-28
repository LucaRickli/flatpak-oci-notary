package indexer

import "testing"

func TestNormalizeRegistryURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://ghcr.io":          "https://ghcr.io",
		"https://ghcr.io/":         "https://ghcr.io",
		" https://quay.io ":        "https://quay.io",
		"http://localhost:5000":    "http://localhost:5000",
		"https://docker.io":        "https://registry-1.docker.io",
		"https://index.docker.io/": "https://registry-1.docker.io",
		"https://ghcr.io/myorg":    "",
		"ghcr.io":                  "",
		"https://user:pw@ghcr.io":  "",
		"ftp://ghcr.io":            "",
	} {
		got, err := NormalizeRegistryURL(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q: expected error, got %q", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestParseAppstream(t *testing.T) {
	info := parseAppstream(`<component type="desktop-application"><name xml:lang="de">Hallo</name><name>Hello</name>` +
		`<summary>Greets</summary><releases><release version="2.0" date="2025-01-01"/><release version="1.0"/></releases></component>`)
	if info.Name != "Hello" || info.Summary != "Greets" || info.Version != "2.0" {
		t.Errorf("unexpected appstream info: %+v", info)
	}
	if parseAppstream("not xml") != (appstreamInfo{}) {
		t.Error("garbage must yield empty info")
	}
}
