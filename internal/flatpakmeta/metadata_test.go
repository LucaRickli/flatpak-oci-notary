package flatpakmeta

import (
	"slices"
	"strings"
	"testing"
)

func TestParseKeyFile(t *testing.T) {
	kf := ParseKeyFile("# leading comment\r\n" +
		"stray=before any group\n" +
		"  [Application]\n" +
		"name = org.example.App\n" +
		"runtime=org.example.Platform/x86_64/24.08   \n" +
		"   # indented comment\n" +
		"not a key value line\n" +
		"name[de]=Beispiel\n" +
		"\n" +
		"[Context]\n" +
		"shared=network;ipc;\n" +
		"filesystems=xdg-download:ro;~/Documents\n" +
		"escaped=a\\sb\\n\\\\c\\q\n" +
		"[Extension org.example.App.Locale]\n" +
		"directory=share/runtime/locale\n" +
		"[Context]\n" +
		"shared=network\n" +
		"[bad\n" +
		"[]\n" +
		"[Extra Data]\n" +
		"uri=https://example.com/a\n" +
		"[Application]\n" +
		"sdk=org.example.Sdk/x86_64/24.08\n" +
		"empty=\n")
	if got := kf.Groups(); !slices.Equal(got, []string{"Application", "Context", "Extension org.example.App.Locale", "Extra Data"}) {
		t.Errorf("groups = %q", got)
	}
	for _, c := range []struct{ group, key, want string }{
		{"Application", "name", "org.example.App"},
		{"Application", "runtime", "org.example.Platform/x86_64/24.08   "}, // GLib keeps trailing whitespace
		{"Application", "sdk", "org.example.Sdk/x86_64/24.08"},             // a reopened group keeps its keys
		{"Application", "empty", ""},
		{"Context", "shared", "network"}, // later duplicate wins
		{"Context", "filesystems", "xdg-download:ro;~/Documents"},
		{"Context", "escaped", "a b\n\\c\\q"},
		{"Extension org.example.App.Locale", "directory", "share/runtime/locale"},
		{"Extra Data", "uri", "https://example.com/a"},
	} {
		if got, ok := kf.Get(c.group, c.key); !ok || got != c.want {
			t.Errorf("[%s] %s = %q (%v), want %q", c.group, c.key, got, ok, c.want)
		}
	}
	for _, c := range []struct{ group, key string }{
		{"Application", "name[de]"}, {"Application", "stray"}, {"", "stray"}, {"Application", "not a key value line"},
		{"bad", ""}, {"", ""}, {"Context", "nothing"},
	} {
		if v, ok := kf.Get(c.group, c.key); ok {
			t.Errorf("[%s] %s unexpectedly set to %q", c.group, c.key, v)
		}
	}
	if kf.HasGroup("bad") || kf.HasGroup("") || !kf.HasGroup("Extra Data") {
		t.Error("group presence wrong")
	}
	if got := kf.Keys("Context"); !slices.Equal(got, []string{"escaped", "filesystems", "shared"}) {
		t.Errorf("keys = %q", got)
	}
	if kf := ParseKeyFile(""); len(kf.Groups()) != 0 {
		t.Error("empty input has groups")
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		data string
		want Metadata
	}{
		{"empty", "", Metadata{}},
		{"garbage", "hello world\n=\n[", Metadata{}},
		{"app", "[Application]\nname=org.example.App\nruntime=org.example.Platform/x86_64/24.08\nsdk=org.example.Sdk/x86_64/24.08\ncommand=app\n\n[Context]\nshared=network;\n",
			Metadata{Runtime: "org.example.Platform/x86_64/24.08"}},
		{"runtime group", "[Runtime]\nname=org.fedoraproject.Platform.Codecs.openh264\nruntime=org.fedoraproject.Platform/x86_64/f44\nsdk=org.fedoraproject.Sdk/x86_64/f44\n\n[Extra Data]\nname=openh264.x86_64.rpm\nchecksum=ddd3\nsize=445460\nuri=https://codecs.fedoraproject.org/openh264/44/x86_64/Packages/o/openh264-2.6.0-3.fc44.x86_64.rpm\n",
			Metadata{Runtime: "org.fedoraproject.Platform/x86_64/f44", HasExtraData: true}},
		{"application wins over runtime group", "[Runtime]\nruntime=org.a/x86_64/1\n[Application]\nruntime=org.b/x86_64/2\n",
			Metadata{Runtime: "org.b/x86_64/2"}},
		{"extension", "[Runtime]\nname=org.example.App.Plugin\nruntime=org.example.Platform/x86_64/24.08\n[ExtensionOf]\nref=app/org.example.App/x86_64/stable\npriority=1\n",
			Metadata{Runtime: "org.example.Platform/x86_64/24.08", ExtensionOf: "app/org.example.App/x86_64/stable"}},
		{"extension-of runtime fallback", "[Runtime]\nname=org.example.Ext\n[ExtensionOf]\nref=runtime/org.example.Platform/x86_64/24.08\nruntime=org.example.Platform/x86_64/24.08\n",
			Metadata{Runtime: "org.example.Platform/x86_64/24.08", ExtensionOf: "runtime/org.example.Platform/x86_64/24.08"}},
		{"platform without runtime", "[Runtime]\nname=org.example.Platform\nsdk=org.example.Sdk/x86_64/24.08\n", Metadata{}},
		{"extra data with suffixes", "[Application]\nname=a\nruntime=r/x86_64/1\n[Extra Data]\nname0=a.tar\nchecksum0=00\nsize0=1\nuri0=https://example.com/a.tar\nname1=b.tar\nuri1=https://example.com/b.tar\n",
			Metadata{Runtime: "r/x86_64/1", HasExtraData: true}},
		{"extra data name only", "[Application]\nname=a\n[Extra Data]\nname3=x\n", Metadata{HasExtraData: true}},
		{"extra data group without entries", "[Application]\nname=a\n[Extra Data]\nNoRuntime=true\nchecksum=00\nuri=\nuriX=https://example.com\nurl=https://example.com\n", Metadata{}},
		{"comments and whitespace", "  # c\n\t[Application]  \n  runtime =  org.example.Platform/x86_64/1\n# runtime=org.other/x86_64/1\n\n", Metadata{Runtime: "org.example.Platform/x86_64/1"}},
		{"localized keys ignored", "[Application]\nruntime[de]=org.de/x86_64/1\n", Metadata{}},
		{"malformed runtime dropped", "[Application]\nruntime=org.example.Platform\n[ExtensionOf]\nref=org.example.App/x86_64/stable\n", Metadata{}},
		{"trailing space is not a ref", "[Application]\nruntime=org.example.Platform/x86_64/1 \n", Metadata{}},
		{"empty component dropped", "[Application]\nruntime=org.example.Platform//1\n", Metadata{}},
		{"nul dropped", "[Application]\nruntime=org.example.Pl\x00atform/x86_64/1\n", Metadata{}},
		{"too long dropped", "[Application]\nruntime=" + strings.Repeat("x", 2000) + "/x86_64/1\n", Metadata{}},
	}
	for _, c := range cases {
		if got := Parse(c.data); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
