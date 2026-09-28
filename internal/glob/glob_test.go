package glob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"", "anything", true},
		{"*", "org/app", true},
		{"**", "org/app", true},
		{"org/*", "org/app", true},
		{"org/*", "other/app", false},
		{"app/org.gnome.*", "app/org.gnome.Maps/x86_64/stable", true},
		{"app/org.gnome.*", "runtime/org.gnome.Platform/x86_64/47", false},
		{"*/x86_64/*", "app/org.example.App/x86_64/stable", true},
		{"v?", "v1", true},
		{"v?", "v10", false},
		{"latest", "latest", true},
		{"latest", "latest2", false},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.s); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}
