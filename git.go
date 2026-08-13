package version

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// releaseTagRe matches release tags — and only release tags. Pre-release /
// qualifier tags (v1.3.0-rc1) and anything else are invisible to the version
// computation by design: a clean version comes from exactly one shape of tag.
var releaseTagRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// Compute derives the version of the git checkout at dir. The rules
// (the README):
//
//   - HEAD carries an annotated release tag vX.Y.Z → "X.Y.Z" (clean release)
//   - commits past the latest annotated release tag → "X.Y.(Z+1)-<sha8>"
//     (the next patch in development; the sha makes every commit's version —
//     and therefore every image tag — unique)
//   - no annotated release tag reachable from HEAD → "0.0.0-<sha8>"
//
// Only annotated tags count. A lightweight tag never produces a clean
// release version — CI's tag-guard job turns that mismatch into a hard
// failure rather than silently publishing a sha-suffixed "release".
// Uncommitted changes are not reflected: the version describes HEAD.
func Compute(dir string) (string, error) {
	sha, err := gitOut(dir, "rev-parse", "--short=8", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolving HEAD (not a git checkout, or no commits yet?): %w", err)
	}
	tag, major, minor, patch, err := latestReleaseTag(dir)
	if err != nil {
		return "", err
	}
	if tag == "" {
		return "0.0.0-" + sha, nil
	}
	onTag, err := headIsAt(dir, tag)
	if err != nil {
		return "", err
	}
	if onTag {
		return strings.TrimPrefix(tag, "v"), nil
	}
	return fmt.Sprintf("%d.%d.%d-%s", major, minor, patch+1, sha), nil
}

// Bump derives the next release tag from the latest annotated release tag
// reachable from HEAD: v1.2.4 → v1.3.0 (Minor), v1.2.5 (Patch), v2.0.0
// (Major). A repo with no release tag yet bumps from v0.0.0. Bump only
// computes the name — Tag creates it, Push publishes it.
func Bump(dir string, level Level) (string, error) {
	if !level.IsValid() {
		return "", fmt.Errorf("invalid bump level %q (want major|minor|patch)", level.String())
	}
	_, major, minor, patch, err := latestReleaseTag(dir)
	if err != nil {
		return "", err
	}
	switch level {
	case Major:
		return fmt.Sprintf("v%d.0.0", major+1), nil
	case Minor:
		return fmt.Sprintf("v%d.%d.0", major, minor+1), nil
	default: // Patch
		return fmt.Sprintf("v%d.%d.%d", major, minor, patch+1), nil
	}
}

// Tag creates the annotated release tag name on HEAD, locally only — a bump
// can't release by accident; pushing the tag is the release act. Annotated
// (not lightweight) is load-bearing: Compute only honors annotated tags.
func Tag(dir, name string) error {
	if !releaseTagRe.MatchString(name) {
		return fmt.Errorf("refusing to create non-release tag %q (want vX.Y.Z)", name)
	}
	_, err := gitOut(dir, "tag", "-a", name, "-m", "Release "+name)
	return err
}

// Push publishes the tag to origin, which is what triggers the CI release
// pipeline.
func Push(dir, name string) error {
	_, err := gitOut(dir, "push", "origin", name)
	return err
}

// latestReleaseTag returns the highest annotated release tag reachable from
// HEAD, with its parsed components. tag == "" (and nil error) means no
// release tag is reachable.
func latestReleaseTag(dir string) (tag string, major, minor, patch int, err error) {
	// for-each-ref gives the object type (annotated tags are "tag" objects,
	// lightweight tags are "commit"), --merged restricts to tags reachable
	// from HEAD (so a hotfix line never sees a newer main-line release), and
	// -v:refname sorts numerically (v1.10.0 > v1.9.0).
	out, err := gitOut(dir, "for-each-ref", "refs/tags",
		"--merged", "HEAD",
		"--sort=-v:refname",
		"--format=%(objecttype) %(refname:short)")
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("listing tags: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		objType, name, ok := strings.Cut(line, " ")
		if !ok || objType != "tag" {
			continue
		}
		m := releaseTagRe.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		major, _ = strconv.Atoi(m[1])
		minor, _ = strconv.Atoi(m[2])
		patch, _ = strconv.Atoi(m[3])
		return name, major, minor, patch, nil
	}
	return "", 0, 0, 0, nil
}

// headIsAt reports whether HEAD is the commit the (annotated) tag points at.
func headIsAt(dir, tag string) (bool, error) {
	head, err := gitOut(dir, "rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	// ^{commit} peels the annotated tag object to the commit it tags.
	tagged, err := gitOut(dir, "rev-parse", tag+"^{commit}")
	if err != nil {
		return false, err
	}
	return head == tagged, nil
}

// gitOut runs git with args in dir and returns trimmed stdout, folding
// stderr into the error for diagnosability.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
