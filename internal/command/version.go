package command

import "runtime/debug"

// Version is yottactl's release, its own and not Desktop's (plan 333 step 5).
// The release build stamps it in with
// -ldflags "-X github.com/YottaBot-Platform/yottactl/internal/command.Version=<version>".
// Otherwise it is dev, and the root command shows the module's version when
// Go knows it, as for `go install github.com/YottaBot-Platform/yottactl/cmd/yottactl@v1.2.3`.
var Version = "dev"

// version is Version, or for an unstamped build the module version Go
// recorded (v1.2.3 shows as 1.2.3), or dev.
func version() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; len(v) > 1 && v[0] == 'v' {
			return v[1:]
		}
	}
	return Version
}
