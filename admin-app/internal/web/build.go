package web

import (
	"strings"
	"time"
)

// Stamped at link time (-ldflags "-X ...") by the Dockerfile, from build args
// that the deploy workflow and `make fly-deploy` pass in. They cannot come from
// runtime/debug.ReadBuildInfo: fly builds with admin-app/ as the context, so
// there is no .git for Go to read the revision from.
//
// Empty in `go run` and in tests, which the footer shows as a development
// build rather than as a missing value.
var (
	buildCommit string // full SHA, "-dirty" appended for an uncommitted tree
	buildTime   string // RFC 3339, UTC
)

// BuildInfo is what the footer shows, so a maintainer can tell which change is
// actually running without opening fly or GitHub Actions.
type BuildInfo struct {
	Commit string // full SHA without the dirty suffix; empty for a dev build
	Short  string
	Dirty  bool
	Time   string // "2026-09-14 11:25 UTC", or the raw value if unparseable
}

func currentBuild() BuildInfo {
	b := BuildInfo{}
	commit, dirty := strings.CutSuffix(buildCommit, "-dirty")
	b.Commit, b.Dirty = commit, dirty
	b.Short = commit
	if len(b.Short) > 7 {
		b.Short = b.Short[:7]
	}
	if t, err := time.Parse(time.RFC3339, buildTime); err == nil {
		b.Time = t.UTC().Format("2006-01-02 15:04 UTC")
	} else {
		b.Time = buildTime
	}
	return b
}
