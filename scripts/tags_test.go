package scripts

import (
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	semverTag        = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
	changelogHeading = regexp.MustCompile(`(?m)^## (v\d+\.\d+\.\d+) - `)
)

// versionKey orders vMAJOR.MINOR.PATCH numerically ("v0.10.0" is newer than "v0.9.0").
func versionKey(v string) int {
	m := semverTag.FindStringSubmatch(v)
	if m == nil {
		return -1
	}
	n := func(s string) int {
		x := 0
		for _, c := range s {
			x = x*10 + int(c-'0')
		}
		return x
	}
	// Six digits per part: no collisions until a part passes 999,999.
	return n(m[1])*1_000_000_000_000 + n(m[2])*1_000_000 + n(m[3])
}

// checkTags compares release tags with the changelog and returns what is wrong.
//
//   - A tag with no section means release notes were forgotten.
//   - A section with no tag is fine if it is newer than every tag (a release being
//     prepared), but not if it is older than the newest tag: everything before the
//     latest release has been released, so a missing tag there is a typo or a lost tag.
//
// Tags that are not plain vX.Y.Z are ignored. No tags at all (a shallow clone)
// means there is nothing to compare, so nothing is reported.
func checkTags(changelog string, tags []string) []string {
	var releases []string
	for _, t := range tags {
		if semverTag.MatchString(t) {
			releases = append(releases, t)
		}
	}
	if len(releases) == 0 {
		return nil
	}
	sections := map[string]bool{}
	for _, m := range changelogHeading.FindAllStringSubmatch(changelog, -1) {
		sections[m[1]] = true
	}
	tagged := map[string]bool{}
	newest := -1
	for _, t := range releases {
		tagged[t] = true
		newest = max(newest, versionKey(t))
	}

	var problems []string
	sort.Strings(releases)
	for _, t := range releases {
		if !sections[t] {
			problems = append(problems, "tag "+t+" has no section in CHANGELOG.md; add '## "+t+" - YYYY-MM-DD'")
		}
	}
	var listed []string
	for s := range sections {
		listed = append(listed, s)
	}
	sort.Strings(listed)
	for _, s := range listed {
		if !tagged[s] && versionKey(s) <= newest {
			problems = append(problems, "CHANGELOG.md has a section for "+s+" but there is no such tag, and it is older than the newest tag")
		}
	}
	return problems
}

func TestCheckTags(t *testing.T) {
	cl := func(vs ...string) string {
		var b strings.Builder
		for _, v := range vs {
			b.WriteString("## " + v + " - 2026-01-01\n\ntext\n\n")
		}
		return b.String()
	}
	cases := []struct {
		name      string
		changelog string
		tags      []string
		want      []string // substrings, one per expected problem
	}{
		{"everything matches", cl("v0.2.0", "v0.1.0"), []string{"v0.1.0", "v0.2.0"}, nil},
		{"a tag with no section", cl("v0.1.0"), []string{"v0.1.0", "v0.2.0"}, []string{"tag v0.2.0 has no section"}},
		{"several tags missing, reported in order", cl("v0.3.0"), []string{"v0.1.0", "v0.2.0", "v0.3.0"},
			[]string{"tag v0.1.0 has no section", "tag v0.2.0 has no section"}},
		{"a section being prepared for the next release is fine", cl("v0.3.0", "v0.2.0", "v0.1.0"), []string{"v0.1.0", "v0.2.0"}, nil},
		{"a section older than the newest tag with no tag of its own is a problem", cl("v0.3.0", "v0.2.0", "v0.1.0"), []string{"v0.1.0", "v0.3.0"},
			[]string{"section for v0.2.0 but there is no such tag"}},
		{"a typo'd version in the changelog", cl("v0.2.0", "v0.1.1"), []string{"v0.1.0", "v0.2.0"},
			[]string{"tag v0.1.0 has no section", "section for v0.1.1 but there is no such tag"}},
		{"versions compare numerically, not as text", cl("v0.10.0", "v0.9.0"), []string{"v0.9.0", "v0.10.0"}, nil},
		{"a prepared v0.10.0 is newer than v0.9.0", cl("v0.10.0", "v0.9.0"), []string{"v0.9.0"}, nil},
		{"tags that are not plain versions are ignored", cl("v0.1.0"), []string{"v0.1.0", "v0.2.0-rc1", "latest", "nightly", "v1"}, nil},
		{"no tags at all (a shallow clone): nothing to compare", cl("v0.1.0"), nil, nil},
		{"only non-version tags: nothing to compare", "", []string{"latest"}, nil},
		{"an empty changelog with real tags", "", []string{"v0.1.0"}, []string{"tag v0.1.0 has no section"}},
		{"a heading inside prose is not a section", "Mentioned ## v0.1.0 - in passing\n", []string{"v0.1.0"}, []string{"tag v0.1.0 has no section"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkTags(c.changelog, c.tags)
			if len(got) != len(c.want) {
				t.Fatalf("problems = %q, want %d matching %q", got, len(c.want), c.want)
			}
			for i, w := range c.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}

func TestVersionKeyOrdering(t *testing.T) {
	order := []string{"v0.1.0", "v0.1.1", "v0.1.9", "v0.1.10", "v0.1.99", "v0.1.100", "v0.2.0", "v0.9.0", "v0.10.0",
		"v0.99.0", "v0.100.0", "v1.0.0", "v1.0.1", "v1.2.3", "v1.10.0", "v10.0.0"}
	for i := 1; i < len(order); i++ {
		if versionKey(order[i-1]) >= versionKey(order[i]) {
			t.Errorf("%s should sort before %s", order[i-1], order[i])
		}
	}
	if versionKey("latest") != -1 || versionKey("v1") != -1 || versionKey("v0.1.0-rc1") != -1 {
		t.Error("non-versions must not parse")
	}
}

// The real thing: this repository's own tags against its own CHANGELOG.md. It
// needs the tags to be present, so it is skipped in a shallow clone (as CI makes).
func TestEveryReleaseTagHasAChangelogSection(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	out, err := exec.Command("git", "-C", "..", "tag", "-l").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	tags := strings.Fields(string(out))
	// Go caches passing tests and cannot see what `git tag` prints. Reading git's
	// tag storage makes the cache notice when a tag is added or removed (loose
	// tags live in refs/tags, packed ones in packed-refs), so this never reports a
	// stale pass right after you tag a release.
	_, _ = os.ReadDir("../.git/refs/tags")
	_, _ = os.ReadFile("../.git/packed-refs")
	var releases int
	for _, tg := range tags {
		if semverTag.MatchString(tg) {
			releases++
		}
	}
	if releases == 0 {
		t.Skip("no release tags here (a shallow clone does not fetch them); run this locally")
	}
	b, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range checkTags(string(b), tags) {
		t.Error(p)
	}
}
