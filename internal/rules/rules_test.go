package rules

import "testing"

func TestRuleMatches(t *testing.T) {
	csharp := Rule{Name: "C#/.NET", Markers: []string{"*.csproj", "*.sln"}}
	if !csharp.Matches([]string{"App.csproj", "Program.cs"}) {
		t.Error("expected glob marker *.csproj to match App.csproj")
	}
	if csharp.Matches([]string{"main.go"}) {
		t.Error("did not expect C# rule to match a Go directory")
	}

	goRule := Rule{Name: "Go", Markers: []string{"go.mod"}}
	if !goRule.Matches([]string{"go.mod", "main.go"}) {
		t.Error("expected literal marker go.mod to match")
	}

	android := Rule{
		Name:        "Android",
		Markers:     []string{"settings.gradle", "settings.gradle.kts"},
		AlsoRequire: []string{"gradlew"},
	}
	if android.Matches([]string{"settings.gradle"}) {
		t.Error("Android must require gradlew too")
	}
	if !android.Matches([]string{"settings.gradle", "gradlew"}) {
		t.Error("Android should match with settings.gradle + gradlew")
	}
}

func TestProjectRulesNonEmpty(t *testing.T) {
	if len(ProjectRules) < 9 {
		t.Fatalf("expected at least 9 built-in rules, got %d", len(ProjectRules))
	}
	for _, r := range ProjectRules {
		if r.Name == "" || len(r.Markers) == 0 || len(r.Artifacts) == 0 {
			t.Errorf("rule %+v is incompletely defined", r)
		}
	}
}
