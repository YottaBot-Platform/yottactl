package command

import (
	"bytes"
	"testing"
)

// yottactl --version prints its release (plan 333 step 5).
func TestVersionFlagPrintsTheRelease(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })
	Version = "1.2.3"
	var out bytes.Buffer
	root := NewRoot()
	root.SetOut(&out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "yottactl 1.2.3\n" {
		t.Fatalf("--version printed %q", out.String())
	}
}

// A build from source isn't stamped: it shows the module's version when Go
// installed it at one (go install ...@v1.2.3), and dev otherwise, as here.
func TestUnstampedBuildIsDev(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("an unstamped build's Version is %q, not dev", Version)
	}
	if got := version(); got != "dev" {
		t.Fatalf("version() in a test build is %q, not dev", got)
	}
	saved := Version
	t.Cleanup(func() { Version = saved })
	Version = "1.2.3"
	if got := version(); got != "1.2.3" {
		t.Fatalf("a stamped build's version() is %q", got)
	}
}
