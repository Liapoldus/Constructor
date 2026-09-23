package infrastructure

import "testing"

func TestProjectProcessEnvironmentUsesAllowlist(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"HOME=/home/constructor",
		"TMPDIR=/tmp",
		"LANG=en_US.UTF-8",
		"GATEWAY_TOKEN=secret",
		"GITHUB_TOKEN=secret",
		"NODE_OPTIONS=--require=/tmp/inject.js",
		"CONSTRUCTOR_DB=/tmp/constructor.db",
		"MALFORMED",
	}
	got := projectProcessEnvironment(parent)
	want := []string{"PATH=/usr/bin", "HOME=/home/constructor", "TMPDIR=/tmp", "LANG=en_US.UTF-8"}
	if len(got) != len(want) {
		t.Fatalf("environment=%q, want only allowlisted values %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("environment[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}
