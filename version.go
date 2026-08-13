// Package version implements git-tag-derived versioning: the version of a
// build is computed from annotated git release tags at build time — never
// hardcoded, never stored in a file, never committed by tooling. Exactly on
// an annotated tag vX.Y.Z the version is the clean "X.Y.Z"; between releases
// it is the next patch plus the commit sha ("X.Y.(Z+1)-<sha8>"); with no
// release tag reachable it is "0.0.0-<sha8>". Cutting a release is nothing
// more than pushing the next annotated tag, which Bump derives and Tag
// creates.
//
// See the README for the full standard, including Makefile, GitLab CI, and
// GitHub Actions recipes.
package version

// Version is the semantic version of this build, stamped at link time via
//
//	-ldflags "-X github.com/activatedio/go-version.Version=<v>"
//
// with the value computed by Compute. Every consuming product stamps this
// one symbol and reads it back (e.g. as cobra.Command.Version and a startup
// log field). A binary built without the stamp — a bare `go build` /
// `go run` during development — reports the zero version below.
var Version = "0.0.0-dev"
