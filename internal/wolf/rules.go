// This file holds the built-in rule table mapping project types to the build
// artifacts they produce, plus the global per-user cache locations. It is pure
// data plus a small matcher, all internal to package wolf.
package wolf

import "path/filepath"

// Rule maps a kind of project to the build artifacts it produces. A rule
// matches a directory when at least one Marker is present in that directory
// and, if AlsoRequire is non-empty, at least one of those is present too.
//
// Markers and Artifacts may be globs (filepath.Match syntax). An Artifact may
// also contain a path separator (e.g. "app/build") to name a nested directory
// relative to the project root.
type Rule struct {
	Name        string   // informational label, e.g. "C#/.NET"
	Markers     []string // filenames or globs identifying the project
	AlsoRequire []string // optional: at least one must also be present
	Artifacts   []string // directories to delete (name, glob, or relative path)
}

// ProjectRules is the built-in, research-backed rule table. Artifact lists
// follow the canonical github/gitignore templates for each ecosystem (and the
// Crystal/Deno docs); see the design spec under docs/superpowers/specs for the
// per-ecosystem sources.
//
// Note there is deliberately no local rule for Go: a Go checkout has no
// canonical reclaimable directory (binaries are loose files, vendor/ is usually
// committed) — its reclaimable space lives in the global module and build caches
// (see GlobalCacheDefs).
var ProjectRules = []Rule{
	{
		Name:    "C#/.NET",
		Markers: []string{"*.csproj", "*.sln", "*.fsproj", "*.vbproj"}, Artifacts: []string{"bin", "obj"},
	},
	{
		Name:    "JavaScript/TS",
		Markers: []string{"package.json"}, Artifacts: []string{"node_modules", "dist", "build", ".next", ".nuxt", "out", ".output", ".svelte-kit", ".parcel-cache", ".turbo", ".vite", "coverage", ".cache"},
	},
	{
		Name:    "Deno",
		Markers: []string{"deno.json", "deno.jsonc", "deno.lock"}, Artifacts: []string{"node_modules", "vendor"},
	},
	{
		Name:    "Rust",
		Markers: []string{"Cargo.toml"}, Artifacts: []string{"target"},
	},
	{
		Name:    "Maven",
		Markers: []string{"pom.xml"}, Artifacts: []string{"target"},
	},
	{
		Name:    "Gradle",
		Markers: []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}, Artifacts: []string{"build", ".gradle"},
	},
	{
		Name:    "Android",
		Markers: []string{"settings.gradle", "settings.gradle.kts"}, AlsoRequire: []string{"gradlew"}, Artifacts: []string{"build", ".gradle", "app/build", ".cxx", ".externalNativeBuild", "captures"},
	},
	{
		Name:    "Flutter/Dart",
		Markers: []string{"pubspec.yaml"}, Artifacts: []string{"build", ".dart_tool"},
	},
	{
		Name:    "Ruby",
		Markers: []string{"Gemfile", "*.gemspec"}, Artifacts: []string{"vendor/bundle", ".bundle", ".yardoc", "coverage", "pkg"},
	},
	{
		Name:    "Python",
		Markers: []string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt"}, Artifacts: []string{"__pycache__", ".venv", "venv", "*.egg-info", ".eggs", "build", "dist", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".tox", ".nox", "htmlcov", ".hypothesis"},
	},
	{
		Name:    "Ruff",
		Markers: []string{"ruff.toml", ".ruff.toml"}, Artifacts: []string{".ruff_cache"},
	},
	{
		Name:    "Crystal",
		Markers: []string{"shard.yml"}, Artifacts: []string{"lib", ".shards", "bin"},
	},
}

// anyMatch reports whether any name matches any of the patterns (filepath.Match
// handles both literal names and globs).
func anyMatch(patterns, names []string) bool {
	for _, p := range patterns {
		for _, n := range names {
			if ok, _ := filepath.Match(p, n); ok {
				return true
			}
		}
	}
	return false
}

// Matches reports whether the rule applies to a directory whose immediate entry
// names are given.
func (r Rule) Matches(names []string) bool {
	if !anyMatch(r.Markers, names) {
		return false
	}
	if len(r.AlsoRequire) > 0 && !anyMatch(r.AlsoRequire, names) {
		return false
	}
	return true
}

// GlobalCacheDef defines a package-manager cache location by its conventional
// path relative to a home directory. findGlobal joins RelPath onto the scanned
// tree (e.g. a backup of a home directory), so caches are matched within that
// tree rather than in the real home directory.
type GlobalCacheDef struct {
	Name    string // informational label, e.g. "Maven"
	RelPath string // path relative to the (scanned) home directory
}

// GlobalCacheDefs is the built-in list of global cache locations.
var GlobalCacheDefs = []GlobalCacheDef{
	{Name: "Maven", RelPath: ".m2/repository"},
	{Name: "Ivy", RelPath: ".ivy2/cache"},
	{Name: "Gradle", RelPath: ".gradle/caches"},
	{Name: "NuGet", RelPath: ".nuget/packages"},
	{Name: "npm", RelPath: ".npm"},
	{Name: "Yarn", RelPath: ".cache/yarn"},
	{Name: "pnpm", RelPath: ".local/share/pnpm/store"},
	{Name: "pip", RelPath: ".cache/pip"},
	{Name: "Cargo registry", RelPath: ".cargo/registry"},
	{Name: "Cargo git", RelPath: ".cargo/git"},
	{Name: "Pub", RelPath: ".pub-cache"},
	{Name: "Deno", RelPath: ".cache/deno"},
	{Name: "Gem", RelPath: ".gem"},
	{Name: "Crystal shards", RelPath: ".cache/shards"},
	{Name: "Go module cache", RelPath: "go/pkg/mod"},
	{Name: "Go build cache", RelPath: ".cache/go-build"},
}
