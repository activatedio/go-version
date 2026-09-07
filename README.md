# go-version

Git-tag-derived versioning for Go projects: the version of a build is
computed from **annotated git release tags** at build time — never
hardcoded, never stored in a file, never committed by tooling. **Cutting a
release is nothing more than pushing an annotated tag.** No release
commits, no version-file bumps, no tooling noise in the git log.

This is the Go implementation of the same model jgitver provides for Maven
(paired there with `io.activated:release-maven-plugin` for the bump).

## The rules

| Git state | Example | Computed version |
|---|---|---|
| On an **annotated** tag `vX.Y.Z` | `v1.5.10` | `1.5.10` (clean release) |
| Any commit after the last release tag | past `v1.5.10` | `1.5.11-a1b2c3d4` (next patch + sha8) |
| Same, on a branch | past `v1.5.10` on `feat/x` | `1.5.11-a1b2c3d4` (same form — the sha identifies the commit) |
| No release tags reachable | fresh repo | `0.0.0-<sha8>` |

Three things to internalize:

- **Releases must be _annotated_ tags** (`git tag -a`, or `bump` which does
  it for you). Lightweight tags — and any non-`vX.Y.Z` tag, like
  `v1.3.0-rc1` — are invisible to the computation. Your CI release job
  should guard on this (recipes below) so a lightweight tag fails loudly
  instead of publishing a sha-suffixed "release".
- After releasing `1.5.10`, mainline builds roll to `1.5.11-<sha8>` — the
  *next patch* in development. Every commit's sha suffix makes every
  artifact tag unique and immutable.
- Only tags **reachable from HEAD** count. A hotfix branch forked from
  `v1.5.0` versions against `v1.5.x` even after main has released `v1.6.0`.

## Library

```go
import version "github.com/activatedio/go-version"

v, err := version.Compute(".")            // "1.5.11-a1b2c3d4"
next, err := version.Bump(".", version.Minor) // "v1.6.0"
err = version.Tag(".", next)              // annotated tag, local only
err = version.Push(".", next)             // the release act
```

`version.Version` is the link-time stamp target (defaults to `0.0.0-dev`).
Wire it into your binary — e.g. with cobra:

```go
cmd := &cobra.Command{Use: "myapp", Version: version.Version}
log.Info().Str("version", version.Version).Msg("starting")
```

## CLI

Add the tool to your module (Go 1.24+):

```bash
go get -tool github.com/activatedio/go-version/cmd/go-version
```

```bash
go tool go-version                     # print this checkout's version
go tool go-version bump                # cut the next minor tag (LOCAL only)
go tool go-version bump -level=patch   # v1.5.10 -> v1.5.11
go tool go-version bump -dry-run       # preview
go tool go-version bump -push          # tag + push = release
```

## Make

The standard Makefile block:

```make
# The git-derived version of this checkout: clean X.Y.Z exactly on an
# annotated release tag, X.Y.(Z+1)-<sha8> between releases. Lazily computed
# on first use; CI overrides it (`make <target> VERSION=...`) with the value
# the pipeline computed once up front.
VERSION ?= $(shell go tool go-version)

version:
	@go tool go-version

# Cut the next release tag (annotated, LOCAL ONLY — pushing the tag is the
# release act).
#   make bump                  # minor (default): v1.5.10 -> v1.6.0
#   make bump LEVEL=patch      #                  v1.5.10 -> v1.5.11
#   make bump LEVEL=major      #                  v1.5.10 -> v2.0.0
#   make bump DRY_RUN=true     # preview only
#   make bump PUSH=true        # tag + push (triggers the release pipeline)
LEVEL ?= minor
bump:
	go tool go-version bump -level=$(LEVEL) $(if $(filter true,$(DRY_RUN)),-dry-run) $(if $(filter true,$(PUSH)),-push)
```

Stamp binaries wherever you build them:

```make
build:
	go build -ldflags "-X github.com/activatedio/go-version.Version=$(VERSION)" -o bin/myapp ./cmd/myapp
```

In a Dockerfile the build context usually has no `.git`, so pass the value
in:

```dockerfile
ARG VERSION=0.0.0-dev
RUN go build -ldflags "-X github.com/activatedio/go-version.Version=${VERSION}" -o /myapp ./cmd/myapp
```

```make
publish:
	docker build --build-arg VERSION=$(VERSION) --tag $(IMAGE):$(VERSION) .
	docker push $(IMAGE):$(VERSION)
```

## What artifacts get published, and when

| Trigger | Artifact tags |
|---|---|
| Commit to the mainline or a `release-*` branch | `:<X.Y.Z-sha8>` (immutable) **and** `:<ref>-latest` (moving, e.g. `main-latest`) |
| Annotated tag `vX.Y.Z` | `:<X.Y.Z>` (immutable) **and** `:latest` (moving) |
| Other feature branch / MR / PR | none — the pipeline only runs tests + lint |

**Prod pins an immutable `:X.Y.Z`. Dev may track `:main-latest`. Nothing
prod-facing ever uses a moving tag.**

### The bare `:X.Y.Z` must be derived from the trigger, not from the version

This table is a contract, and keeping it takes one deliberate step, because
**`VERSION` alone cannot tell you which row you are in.**

`VERSION` is sha-suffixed only *between* releases. On the commit that a release
tag points at it is the clean `X.Y.Z` — and it is clean on **every** ref that
reaches that commit, because it is computed from git and knows nothing about the
pipeline it runs in. Releasing means pushing the mainline and then the tag, so
two pipelines run on that commit seconds apart and **both compute `X.Y.Z`**.

Publish `:${VERSION}` unconditionally and the mainline pipeline therefore
publishes the release's own artifact tag, in violation of the first row. Whichever
finishes last wins, and container builds are not bit-reproducible, so the digest
changes with it: the tag still resolves, but no longer to what the release
published. A digest recorded against that tag — in release notes, in an
attestation, in a downstream pin derived by reading the tag — silently stops
matching.

So derive the artifact tag from the trigger. Both recipes below do, and the
sha-suffix is applied by stripping any sha `go-version` already added and
re-appending the CI one (`${VERSION%%-*}-${SHORT_SHA}`), so a mainline artifact
has one shape whether or not the commit happens to be tagged.

**This is not container-specific.** Anything keyed by version has the same
collision: a generic package registry path, an npm version, a Maven coordinate.
The Java convention already handles it by construction — the mainline job
publishes `${VERSION}-SNAPSHOT` and only the release job publishes `${VERSION}` —
and that is the same fix in different clothing. Registries that refuse to
overwrite an existing version (npm, Maven Central) turn the collision into a
failed job rather than a silent replacement, which is better but still a broken
release pipeline.

## GitLab CI

The computation needs full history and tags. GitLab clones shallow by
default, which hides tags → misversioned builds (`0.0.0-<sha>`). Set both
variables globally and keep them if you fork the pipeline:

```yaml
variables:
  GIT_DEPTH: "0"
  GIT_FETCH_EXTRA_FLAGS: "--tags"
```

Compute the version once per pipeline and fan it out as a dotenv artifact.
On tag pipelines the same job is the release gate — a lightweight tag
computes as `X.Y.Z-<sha8>`, mismatches the tag, and fails here:

```yaml
version:
  stage: prepare
  image: golang:1.26
  script:
    - VERSION=$(go tool go-version)
    - echo "VERSION=${VERSION}" | tee version.env
    - |
      if [ -n "${CI_COMMIT_TAG}" ] && [ "${VERSION}" != "${CI_COMMIT_TAG#v}" ]; then
        echo "ERROR: computed version ${VERSION} does not match release tag ${CI_COMMIT_TAG}."
        echo "Releases must be ANNOTATED tags: git tag -a ${CI_COMMIT_TAG}  (or make bump)"
        exit 1
      fi
  artifacts:
    reports:
      dotenv: version.env
  rules:
    - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/'
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH || $CI_COMMIT_BRANCH =~ /^release-/'

publish:
  stage: publish
  script:
    # ${VERSION} arrives via the dotenv artifact.
    # Both tags come from CI_COMMIT_TAG, and both must: see "The bare :X.Y.Z
    # must be derived from the trigger". ARTIFACT_TAG is NOT ${VERSION} —
    # ${VERSION} is clean on a tagged commit whichever ref is building it, so
    # using it here lets a mainline pipeline overwrite the release's own tag.
    - |
      if [ -n "${CI_COMMIT_TAG}" ]; then
        ARTIFACT_TAG="${VERSION}"
        MOVING_TAG="latest"
      else
        ARTIFACT_TAG="${VERSION%%-*}-${CI_COMMIT_SHORT_SHA}"
        MOVING_TAG="${CI_COMMIT_REF_SLUG}-latest"
      fi
    # VERSION stays the git-derived value: that is what gets stamped into the
    # binary, and it should describe the code. ARTIFACT_TAG names the artifact.
    - make publish VERSION="${VERSION}" DOCKER_TAG="${ARTIFACT_TAG}"
    - make publish VERSION="${VERSION}" DOCKER_TAG="${MOVING_TAG}"
  rules:
    - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/'
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH || $CI_COMMIT_BRANCH =~ /^release-/'
```

(Adapt the publish job to your artifact pipeline — multi-arch image builds
typically push a per-arch staging tag, then a manifest job merges the pair into
`:${ARTIFACT_TAG}` and `:${MOVING_TAG}`. **Scope the staging tags to the
pipeline, not to the version** — `stage-${CI_PIPELINE_ID}-<arch>`, say. Two
pipelines on one commit compute the same `VERSION`, so version-named staging
tags are a shared mutable resource: the first job to finish deletes the inputs
the second still needs, and the release fails with a 404 on a tag the other
pipeline already cleaned up.)

## GitHub Actions

The equivalent gotcha: `actions/checkout` fetches a single commit by
default. Ask for full history and tags:

```yaml
on:
  push:
    branches: [main, "release-*"]
    tags: ["v*.*.*"]
  pull_request:

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0          # full history — the computation needs it
          fetch-tags: true        # ...and the tags
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - name: Compute version (and gate releases)
        id: version
        run: |
          VERSION=$(go tool go-version)
          echo "version=${VERSION}" >> "$GITHUB_OUTPUT"
          TAG="${GITHUB_REF_NAME}"
          if [[ "${GITHUB_REF}" == refs/tags/* && "${VERSION}" != "${TAG#v}" ]]; then
            echo "ERROR: computed version ${VERSION} does not match release tag ${TAG}."
            echo "Releases must be ANNOTATED tags: git tag -a ${TAG}  (or make bump)"
            exit 1
          fi
      - run: make test
      - name: Publish
        if: github.event_name == 'push'
        run: |
          VERSION="${{ steps.version.outputs.version }}"
          # Derived from the trigger, not from VERSION — see "The bare :X.Y.Z
          # must be derived from the trigger".
          if [[ "${GITHUB_REF}" == refs/tags/* ]]; then
            ARTIFACT_TAG="${VERSION}"
            MOVING_TAG="latest"
          else
            ARTIFACT_TAG="${VERSION%%-*}-${GITHUB_SHA::8}"
            MOVING_TAG="${GITHUB_REF_NAME}-latest"
          fi
          make publish VERSION="${VERSION}" DOCKER_TAG="${ARTIFACT_TAG}"
          make publish VERSION="${VERSION}" DOCKER_TAG="${MOVING_TAG}"
```

## How do I cut a release?

Either tag by hand:

```bash
git tag -a v1.6.0 -m "Release v1.6.0"
git push origin v1.6.0
```

...or let `bump` derive the next tag from the latest reachable one:

```bash
make bump                  # local-only annotated tag; can't release by accident
git push origin v1.6.0     # ...or `make bump PUSH=true` to do both
```

## How do I hotfix an older line?

Branch from the old tag, fix, tag the fix:

```bash
git checkout -b release-1.5 v1.5.10
# ... fix ...
git tag -a v1.5.11 -m "Release v1.5.11"
git push origin release-1.5 v1.5.11
```

Version computation on the branch only sees the `v1.5.x` tags —
reachability keeps the lines apart.

## Why do we do it this way?

- **No release-tooling commits** — the git log stays a clean story of real
  changes.
- **Reproducible** — the same commit always computes the same version.
- **Trunk-based friendly** — tagging is the only release ceremony.
- **Trivial rollback** — every mainline commit is a uniquely-tagged,
  immutable artifact.
