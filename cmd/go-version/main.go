// Command go-version prints the git-derived version of the working tree,
// and (with `bump`) cuts the next release tag.
//
//	go tool go-version                 # print this checkout's version
//	go tool go-version bump            # cut the next minor release tag (local only)
//	go tool go-version bump -level=patch -dry-run
//	go tool go-version bump -push      # tag and push in one step
//
// Consumers add it as a go.mod tool dependency and wrap it in `make version`
// / `make bump` (see the README). Only the machine-consumable value (the
// version, or the created tag name) goes to stdout; commentary goes to
// stderr so the output stays scriptable.
package main

import (
	"flag"
	"fmt"
	"os"

	version "github.com/activatedio/go-version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "bump" {
		bump(os.Args[2:])
		return
	}
	v, err := version.Compute(".")
	if err != nil {
		fatal(err)
	}
	fmt.Println(v)
}

// bump derives the next release tag from the latest reachable one and
// creates it as an annotated tag. Local-only by default so a bump can't
// release by accident — pushing the tag is the release act.
func bump(args []string) {
	fs := flag.NewFlagSet("bump", flag.ExitOnError)
	level := fs.String("level", version.Minor.String(), "release level: major|minor|patch")
	dryRun := fs.Bool("dry-run", false, "print the next tag without creating it")
	push := fs.Bool("push", false, "push the tag to origin after creating it (triggers the release pipeline)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	lv := version.ParseLevel(*level)
	if !lv.IsValid() {
		fatal(fmt.Errorf("invalid -level %q (want major|minor|patch)", *level))
	}
	next, err := version.Bump(".", lv)
	if err != nil {
		fatal(err)
	}
	if *dryRun {
		fmt.Fprintf(os.Stderr, "dry run — would create annotated tag:\n")
		fmt.Println(next)
		return
	}
	if err := version.Tag(".", next); err != nil {
		fatal(err)
	}
	if *push {
		if err := version.Push(".", next); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "created and pushed annotated tag (release pipeline triggered):\n")
	} else {
		fmt.Fprintf(os.Stderr, "created annotated tag (local only — push it to release):\n  git push origin %s\n", next)
	}
	fmt.Println(next)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
