package servermanager

import _ "embed"

// RawChangelog is the markdown source of CHANGELOG.md, embedded at build
// time. Render it via the internal/changelog package.
//
//go:embed CHANGELOG.md
var RawChangelog []byte
