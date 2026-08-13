package version_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/activatedio/go-version"
	"github.com/stretchr/testify/require"
)

// repo is a scratch git repository the arrange callbacks build state in.
// Every git invocation is isolated from the developer's real git config
// (identity comes from env, global/system config are disabled) so the tests
// behave identically on a laptop and in CI.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@activated.io",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@activated.io",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	require.NoError(r.t, cmd.Run(), "git %v: %s", args, errb.String())
	return strings.TrimSpace(out.String())
}

func (r *repo) commit(msg string) { r.git("commit", "--allow-empty", "-q", "-m", msg) }

// tagAnnotated creates the tag shape Compute honors.
func (r *repo) tagAnnotated(name string) { r.git("tag", "-a", name, "-m", "Release "+name) }

// tagLightweight creates the tag shape Compute must ignore.
func (r *repo) tagLightweight(name string) { r.git("tag", name) }

func (r *repo) sha8() string { return r.git("rev-parse", "--short=8", "HEAD") }

func TestCompute(t *testing.T) {
	cases := map[string]struct {
		arrange func(t *testing.T) (dir, want string)
		assert  func(t *testing.T, want, got string, err error)
	}{
		"no release tags yet computes zero version plus sha": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				return r.dir, "0.0.0-" + r.sha8()
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, want, got)
			},
		},
		"exactly on an annotated release tag computes the clean version": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.0")
				return r.dir, "1.2.0"
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, want, got)
			},
		},
		"commits after the latest release compute the next patch plus sha": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.0")
				r.commit("in development")
				return r.dir, "1.2.1-" + r.sha8()
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, want, got)
			},
		},
		"a lightweight tag never produces a clean release": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.0")
				r.commit("in development")
				r.tagLightweight("v1.3.0") // on HEAD, but not annotated
				return r.dir, "1.2.1-" + r.sha8()
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, want, got)
			},
		},
		"non-release annotated tags are invisible": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.0")
				r.commit("in development")
				r.tagAnnotated("v1.3.0-rc1") // annotated, but not vX.Y.Z
				return r.dir, "1.2.1-" + r.sha8()
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, want, got)
			},
		},
		"version sort is numeric so v1.10 outranks v1.9": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.9.0")
				r.commit("second")
				r.tagAnnotated("v1.10.0")
				r.commit("in development")
				return r.dir, "1.10.1-" + r.sha8()
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, want, got)
			},
		},
		"a release tag on a branch line sees only its own history": {
			arrange: func(t *testing.T) (string, string) {
				// Hotfix scenario: main released v1.3.0, but the hotfix
				// branch forked from v1.2.0 — its version must derive from
				// v1.2.0, not the unreachable v1.3.0.
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.0")
				r.git("checkout", "-q", "-b", "hotfix/1.2.x")
				r.git("checkout", "-q", "main")
				r.commit("main moves on")
				r.tagAnnotated("v1.3.0")
				r.git("checkout", "-q", "hotfix/1.2.x")
				r.commit("the fix")
				return r.dir, "1.2.1-" + r.sha8()
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, want, got)
			},
		},
		"a directory that is not a git checkout errors": {
			arrange: func(t *testing.T) (string, string) {
				return t.TempDir(), ""
			},
			assert: func(t *testing.T, want, got string, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "resolving HEAD")
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			dir, want := v.arrange(t)
			got, err := version.Compute(dir)
			v.assert(t, want, got, err)
		})
	}
}

func TestBump(t *testing.T) {
	cases := map[string]struct {
		arrange func(t *testing.T) string // returns dir
		level   version.Level
		assert  func(t *testing.T, got string, err error)
	}{
		"minor from v1.2.4": {
			arrange: func(t *testing.T) string {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.4")
				r.commit("work")
				return r.dir
			},
			level: version.Minor,
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, "v1.3.0", got)
			},
		},
		"patch from v1.2.4": {
			arrange: func(t *testing.T) string {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.4")
				return r.dir
			},
			level: version.Patch,
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, "v1.2.5", got)
			},
		},
		"major from v1.2.4": {
			arrange: func(t *testing.T) string {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.2.4")
				return r.dir
			},
			level: version.Major,
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, "v2.0.0", got)
			},
		},
		"minor with no release tag yet starts the line at v0.1.0": {
			arrange: func(t *testing.T) string {
				r := newRepo(t)
				r.commit("first")
				return r.dir
			},
			level: version.Minor,
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, "v0.1.0", got)
			},
		},
		"derives from the highest reachable release tag": {
			arrange: func(t *testing.T) string {
				r := newRepo(t)
				r.commit("first")
				r.tagAnnotated("v1.9.0")
				r.commit("second")
				r.tagAnnotated("v1.10.0")
				return r.dir
			},
			level: version.Minor,
			assert: func(t *testing.T, got string, err error) {
				require.NoError(t, err)
				require.Equal(t, "v1.11.0", got)
			},
		},
		"zero level is rejected": {
			arrange: func(t *testing.T) string {
				r := newRepo(t)
				r.commit("first")
				return r.dir
			},
			level: version.Level{},
			assert: func(t *testing.T, got string, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid bump level")
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			dir := v.arrange(t)
			got, err := version.Bump(dir, v.level)
			v.assert(t, got, err)
		})
	}
}

func TestTagAndPush(t *testing.T) {
	// remoteTags lists the release tags a bare "origin" has received —
	// the observable difference between Tag (local only) and Tag+Push.
	remoteTags := func(t *testing.T, bare string) string {
		t.Helper()
		cmd := exec.Command("git", "tag", "--list", "v*")
		cmd.Dir = bare
		out, err := cmd.Output()
		require.NoError(t, err)
		return strings.TrimSpace(string(out))
	}

	cases := map[string]struct {
		arrange func(t *testing.T) (dir, bare string)
		act     func(t *testing.T, dir string) error
		assert  func(t *testing.T, dir, bare string, err error)
	}{
		"tag creates an annotated release tag locally only": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				bare := filepath.Join(t.TempDir(), "origin.git")
				r.git("init", "-q", "--bare", bare)
				r.git("remote", "add", "origin", bare)
				return r.dir, bare
			},
			act: func(t *testing.T, dir string) error {
				return version.Tag(dir, "v1.0.0")
			},
			assert: func(t *testing.T, dir, bare string, err error) {
				require.NoError(t, err)
				// Annotated on HEAD → Compute sees the clean release.
				got, err := version.Compute(dir)
				require.NoError(t, err)
				require.Equal(t, "1.0.0", got)
				// ...but nothing reached origin: releasing is the push.
				require.Empty(t, remoteTags(t, bare))
			},
		},
		"push publishes the tag to origin": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				bare := filepath.Join(t.TempDir(), "origin.git")
				r.git("init", "-q", "--bare", bare)
				r.git("remote", "add", "origin", bare)
				return r.dir, bare
			},
			act: func(t *testing.T, dir string) error {
				if err := version.Tag(dir, "v1.0.0"); err != nil {
					return err
				}
				return version.Push(dir, "v1.0.0")
			},
			assert: func(t *testing.T, dir, bare string, err error) {
				require.NoError(t, err)
				require.Equal(t, "v1.0.0", remoteTags(t, bare))
			},
		},
		"tag refuses a non-release name": {
			arrange: func(t *testing.T) (string, string) {
				r := newRepo(t)
				r.commit("first")
				return r.dir, ""
			},
			act: func(t *testing.T, dir string) error {
				return version.Tag(dir, "v1.0.0-rc1")
			},
			assert: func(t *testing.T, dir, bare string, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "non-release tag")
			},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			dir, bare := v.arrange(t)
			err := v.act(t, dir)
			v.assert(t, dir, bare, err)
		})
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]struct {
		in    string
		valid bool
	}{
		"major slug parses":                      {in: "major", valid: true},
		"minor slug parses":                      {in: "minor", valid: true},
		"patch slug parses":                      {in: "patch", valid: true},
		"unknown slug is the invalid zero level": {in: "banana", valid: false},
		"empty slug is the invalid zero level":   {in: "", valid: false},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			got := version.ParseLevel(v.in)
			require.Equal(t, v.valid, got.IsValid())
			if v.valid {
				require.Equal(t, v.in, got.String())
			}
		})
	}
}
