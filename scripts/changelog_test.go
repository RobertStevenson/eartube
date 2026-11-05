package scripts

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The changelog is read by people deciding whether to upgrade, so keep its
// structure honest: well-formed headings, newest first, and a link per version.
func TestChangelogIsWellFormed(t *testing.T) {
	b, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	heading := regexp.MustCompile(`(?m)^## (v(\d+)\.(\d+)\.(\d+)) - (\d{4}-\d{2}-\d{2})$`)
	found := heading.FindAllStringSubmatch(text, -1)
	if len(found) < 6 {
		t.Fatalf("found only %d version headings; the format is '## vX.Y.Z - YYYY-MM-DD'", len(found))
	}
	if all := regexp.MustCompile(`(?m)^## `).FindAllString(text, -1); len(all) != len(found) {
		t.Errorf("%d '## ' headings but only %d match the version format", len(all), len(found))
	}

	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	prevKey, prevDate := 1<<60, "9999-99-99"
	seen := map[string]bool{}
	for _, m := range found {
		ver, date := m[1], m[5]
		if seen[ver] {
			t.Errorf("%s appears twice", ver)
		}
		seen[ver] = true
		key := num(m[2])*1_000_000 + num(m[3])*1_000 + num(m[4])
		if key >= prevKey {
			t.Errorf("%s is not older than the version above it; the changelog must be newest first", ver)
		}
		if date > prevDate {
			t.Errorf("%s (%s) is dated after the newer release above it (%s)", ver, date, prevDate)
		}
		prevKey, prevDate = key, date

		if !regexp.MustCompile(`(?m)^\[` + regexp.QuoteMeta(ver) + `\]: https://github\.com/RobertStevenson/eartube/`).MatchString(text) {
			t.Errorf("no link reference for [%s] at the bottom", ver)
		}
		// Each release must say something under at least one heading.
		section := text[strings.Index(text, "## "+ver+" - "):]
		if next := strings.Index(section[3:], "\n## "); next >= 0 {
			section = section[:next+3]
		}
		if !strings.Contains(section, "### ") || len(strings.TrimSpace(section)) < 120 {
			t.Errorf("%s has no real content", ver)
		}
	}
}
