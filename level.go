package version

// Level records which semver component a Bump advances. It is a typesafe
// enum (struct with a private slug): values cannot be constructed outside
// this package, so only the package-level instances below are ever valid.
// The zero Level is invalid — callers parse operator input with ParseLevel
// and check IsValid.
type Level struct {
	slug string
}

// String returns the lowercase slug used on the CLI (`-level=minor`) and in
// log fields. The zero Level stringifies to "".
func (l Level) String() string { return l.slug }

// IsValid reports whether l is one of the defined non-zero values.
func (l Level) IsValid() bool {
	_, ok := levels[l.slug]
	return ok
}

var (
	// Major advances X in vX.Y.Z and resets the rest: v1.2.4 → v2.0.0.
	Major = Level{slug: "major"}

	// Minor advances Y and resets the patch: v1.2.4 → v1.3.0. The default
	// for `bump` — routine feature releases are minor.
	Minor = Level{slug: "minor"}

	// Patch advances Z: v1.2.4 → v1.2.5. For hotfix lines.
	Patch = Level{slug: "patch"}
)

// levels is the parse-by-slug table backing ParseLevel.
var levels = map[string]Level{
	Major.slug: Major,
	Minor.slug: Minor,
	Patch.slug: Patch,
}

// ParseLevel looks up the Level whose slug matches s. Unknown or empty
// strings return the zero Level; callers reject it via IsValid.
func ParseLevel(s string) Level {
	return levels[s]
}
