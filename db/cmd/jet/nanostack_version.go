// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package main

import (
	"fmt"
	"runtime/debug"
)

// upstreamCommit is the go-jet commit Nanostack DB is based on: the Base commit row of
// db/UPSTREAM.md (TestUpstreamCommitMatchesUPSTREAM checks it). version, in the upstream
// file version.go, is the tag go-jet stamped at that commit, kept as upstream wrote it.
const upstreamCommit = "d0e152c"

// versionString identifies the binary as Nanostack DB, with the go-jet base it forks.
//
// The Nanostack DB version comes from the build: `go install
// github.com/nanostack-dev/nanostack-framework/db/cmd/jet@v0.3.0` reports v0.3.0, and a
// build from a checkout reports (devel). Upstream stamped version.go with
// `git describe --tags` through go:generate; in this repository that would name a
// framework tag, not a db release, so the stamp is gone.
func versionString() string {
	return fmt.Sprintf("Nanostack DB %s (fork of go-jet %s, commit %s)", moduleVersion(), version, upstreamCommit)
}

func moduleVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
