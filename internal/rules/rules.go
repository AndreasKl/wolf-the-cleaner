// Package rules holds the built-in table mapping project types to the build
// artifacts they produce, plus the global per-user cache locations.
package rules

import "path/filepath"

// Rule maps a kind of project to the build artifacts it produces. A rule
// matches a directory when at least one Marker is present in that directory
// and, if AlsoRequire is non-empty, at least one of those is present too.
type Rule struct {
	Name        string   // informational label, e.g. "C#/.NET"
	Markers     []string // filenames or globs identifying the project
	AlsoRequire []string // optional: at least one must also be present
	Artifacts   []string // child dirs to delete; may be a glob or contain "/"
}

// ProjectRules is the built-in rule table.
var ProjectRules = []Rule{
	{Name: "C#/.NET", Markers: []string{"*.csproj", "*.sln", "*.fsproj"}, Artifacts: []string{"bin", "obj"}},
	{Name: "JavaScript/TS", Markers: []string{"package.json"}, Artifacts: []string{"node_modules", "dist", "build", ".next", ".nuxt"}},
	{Name: "Rust", Markers: []string{"Cargo.toml"}, Artifacts: []string{"target"}},
	{Name: "Java", Markers: []string{"pom.xml", "build.gradle", "build.gradle.kts"}, Artifacts: []string{"target", "build", ".gradle"}},
	{Name: "Kotlin", Markers: []string{"build.gradle.kts", "*.kts", "settings.gradle", "settings.gradle.kts"}, Artifacts: []string{"build", ".gradle", "out"}},
	{Name: "Android", Markers: []string{"settings.gradle", "settings.gradle.kts"}, AlsoRequire: []string{"gradlew"}, Artifacts: []string{"build", ".gradle", "app/build", ".cxx"}},
	{Name: "Flutter/Dart", Markers: []string{"pubspec.yaml"}, Artifacts: []string{"build", ".dart_tool", ".flutter-plugins", ".packages"}},
	{Name: "Go", Markers: []string{"go.mod"}, Artifacts: []string{"bin"}},
	{Name: "Ruby", Markers: []string{"Gemfile", "*.gemspec"}, Artifacts: []string{"vendor/bundle", ".bundle"}},
	{Name: "Python", Markers: []string{"pyproject.toml", "setup.py", "requirements.txt"}, Artifacts: []string{"__pycache__", ".venv", "venv", "*.egg-info", "build", "dist", ".pytest_cache", ".mypy_cache"}},
	{Name: "Crystal", Markers: []string{"shard.yml"}, Artifacts: []string{"lib", ".shards", "bin"}},
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

// GlobalCacheDef defines a global per-user cache location.
type GlobalCacheDef struct {
	Name     string // informational label, e.g. "Maven"
	RelPath  string // path relative to the user's home directory
	GoEnvKey string // if set, resolved via `go env <key>`, RelPath as fallback
}

// GlobalCacheDefs is the built-in list of global cache locations.
var GlobalCacheDefs = []GlobalCacheDef{
	{Name: "Maven", RelPath: ".m2/repository"},
	{Name: "Ivy", RelPath: ".ivy2/cache"},
	{Name: "Gradle", RelPath: ".gradle/caches"},
	{Name: "NuGet", RelPath: ".nuget/packages"},
	{Name: "npm", RelPath: ".npm"},
	{Name: "Yarn", RelPath: ".cache/yarn"},
	{Name: "pip", RelPath: ".cache/pip"},
	{Name: "Cargo", RelPath: ".cargo/registry"},
	{Name: "Pub", RelPath: ".pub-cache"},
	{Name: "Gem", RelPath: ".gem"},
	{Name: "Go module cache", RelPath: "go/pkg/mod", GoEnvKey: "GOMODCACHE"},
	{Name: "Go build cache", RelPath: ".cache/go-build", GoEnvKey: "GOCACHE"},
}
