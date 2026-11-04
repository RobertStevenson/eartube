package config

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSettingDocsMatchSettingKeysInOrder(t *testing.T) {
	var got []string
	for _, d := range settingDocs {
		got = append(got, d.Key)
		if d.Comment == "" || !strings.HasPrefix(d.Example, d.Key+" = ") {
			t.Errorf("%s: malformed doc %+v", d.Key, d)
		}
	}
	if !reflect.DeepEqual(got, SettingKeys) {
		t.Fatalf("settingDocs keys %v differ from SettingKeys %v", got, SettingKeys)
	}
}

func TestMissingKeys(t *testing.T) {
	all := strings.Join(SettingKeys, "\n")
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"empty file: everything is missing", "", SettingKeys},
		{"the starter template mentions everything", Template(), nil},
		{"active settings count", "browser = \"b\"\ndownloads = true\ndownload_max_gb = 2\ndelete_played = true\ndownload_dir = \"/d\"\n", nil},
		{"commented-out examples count", "# browser = \"\"\n# downloads = false\n# download_max_gb = 5\n# delete_played = false\n# download_dir = \"\"\n", nil},
		{"only browser (the v0.1 file)", "browser = \"brave+gnomekeyring\"\n", []string{"downloads", "download_max_gb", "delete_played", "download_dir"}},
		{"a user's own mix", "browser = \"x\"\n#downloads=true\n  # delete_played = false\n", []string{"download_max_gb", "download_dir"}},
		{"spacing and indentation do not matter", "\t#   browser   =   \"x\"\n   downloads=false\n", []string{"download_max_gb", "delete_played", "download_dir"}},
		{"a setting named in prose is not a mention", "# set downloads to true if you want them\n# see download_dir for the location\n", SettingKeys},
		{"a longer key is not a mention of a shorter one", "download_max_gb = 5\n", []string{"browser", "downloads", "delete_played", "download_dir"}},
		{"a key in the middle of a line is not a mention", "x = 1 # browser = \"y\"\n", SettingKeys},
		{"keys must be at the start of a line", all, SettingKeys}, // bare names, no "="
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MissingKeys(c.content); !reflect.DeepEqual(got, c.want) {
				t.Errorf("MissingKeys = %v, want %v", got, c.want)
			}
		})
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cfg", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddMissingKeepsEverythingAlreadyInTheFile(t *testing.T) {
	mine := "# my notes: keep these!\nbrowser = \"brave+gnomekeyring\"\n\n#   odd   formatting   \n"
	p := writeFile(t, mine)
	added, err := AddMissing(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"downloads", "download_max_gb", "delete_played", "download_dir"}
	if !reflect.DeepEqual(added, want) {
		t.Fatalf("added %v, want %v", added, want)
	}
	got, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(got), mine) {
		t.Fatalf("existing content was changed; the file must only ever grow at the end:\n%s", got)
	}
	for _, k := range want {
		if !strings.Contains(string(got), "# "+k+" = ") {
			t.Errorf("no commented example for %s appended", k)
		}
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("permissions changed to %v", st.Mode().Perm())
	}
}

func TestAddMissingChangesNoSetting(t *testing.T) {
	p := writeFile(t, "browser = \"brave+gnomekeyring\"\ndownloads = true\n")
	before, err := Load(nil, env(map[string]string{"HOME": home}, map[string]string{defaultFile: "browser = \"brave+gnomekeyring\"\ndownloads = true\n"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddMissing(p); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	after, err := Load(nil, env(map[string]string{"HOME": home}, map[string]string{defaultFile: string(data)}), io.Discard)
	if err != nil {
		t.Fatalf("the file no longer loads after adding comments: %v", err)
	}
	b, a := before, after
	b.Sources, a.Sources, b.Path, a.Path = nil, nil, "", ""
	if !reflect.DeepEqual(b, a) {
		t.Errorf("adding comments changed the effective settings:\nbefore %+v\nafter  %+v", b, a)
	}
	if after.Sources["browser"] != "config file" || after.Sources["download_dir"] != "default" {
		t.Errorf("sources changed: %v", after.Sources)
	}
}

func TestAddMissingIsIdempotent(t *testing.T) {
	p := writeFile(t, "browser = \"b\"\n")
	if _, err := AddMissing(p); err != nil {
		t.Fatal(err)
	}
	once, _ := os.ReadFile(p)
	added, err := AddMissing(p)
	if err != nil || len(added) != 0 {
		t.Fatalf("second call = %v, %v; want nothing to add", added, err)
	}
	twice, _ := os.ReadFile(p)
	if string(once) != string(twice) {
		t.Fatal("a second call changed the file")
	}
	if m, _ := MissingInFile(p); len(m) != 0 {
		t.Fatalf("still missing %v after adding", m)
	}
}

func TestAddMissingHandlesAFileWithoutATrailingNewline(t *testing.T) {
	p := writeFile(t, "browser = \"b\"") // no final newline
	if _, err := AddMissing(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(got), "browser = \"b\"\n") {
		t.Fatalf("the user's last line was run into the comments:\n%q", got)
	}
	if _, err := Load(nil, env(map[string]string{"HOME": home}, map[string]string{defaultFile: string(got)}), io.Discard); err != nil {
		t.Fatalf("result does not load: %v", err)
	}
}

// However the file ends, there is exactly one blank line before what we add, and
// the user's last line is never run into it.
func TestAddMissingSeparatesWithExactlyOneBlankLine(t *testing.T) {
	for name, content := range map[string]string{
		"no final newline":    "browser = \"b\"",
		"one final newline":   "browser = \"b\"\n",
		"blank line already":  "browser = \"b\"\n\n",
		"several blank lines": "browser = \"b\"\n\n\n",
		"empty file":          "",
	} {
		t.Run(name, func(t *testing.T) {
			p := writeFile(t, content)
			if _, err := AddMissing(p); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(p)
			i := strings.Index(string(got), "# Added by eartube")
			if i < 0 {
				t.Fatal("marker comment missing")
			}
			before := string(got[:i])
			if !strings.HasPrefix(before, content) {
				t.Fatalf("existing content altered:\n%q", before)
			}
			gap := strings.TrimPrefix(before, strings.TrimRight(content, "\n"))
			switch {
			case content == "":
				if before != "" {
					t.Errorf("empty file should start with the marker, got %q", before)
				}
			case strings.HasSuffix(content, "\n\n\n"):
				if gap != "\n\n\n" { // already more than one blank line: leave the user's spacing alone
					t.Errorf("gap = %q", gap)
				}
			default:
				if gap != "\n\n" {
					t.Errorf("gap between the user's last line and the marker = %q, want one blank line", gap)
				}
			}
		})
	}
}

func TestAddMissingDoesNothingWhenNothingIsMissing(t *testing.T) {
	p := writeFile(t, Template())
	before, _ := os.Stat(p)
	added, err := AddMissing(p)
	if err != nil || len(added) != 0 {
		t.Fatalf("= %v, %v", added, err)
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("the file was rewritten although there was nothing to add")
	}
}

func TestAddMissingNeverCreatesAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.toml")
	if _, err := AddMissing(p); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("AddMissing created a file; that is EnsureFile's job")
	}
}

func TestMissingInFile(t *testing.T) {
	if m, err := MissingInFile(filepath.Join(t.TempDir(), "absent.toml")); err != nil || m != nil {
		t.Errorf("absent file = %v, %v; want nothing to add to", m, err)
	}
	p := writeFile(t, "browser = \"b\"\n")
	m, err := MissingInFile(p)
	if err != nil || len(m) != 4 {
		t.Errorf("= %v, %v", m, err)
	}
	if _, err := MissingInFile(t.TempDir()); err == nil { // a directory is not a readable file
		t.Error("expected an error reading a directory")
	}
}

func TestEveryAppendedBlockIsValidWhenUncommented(t *testing.T) {
	p := writeFile(t, "")
	if _, err := AddMissing(p); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	var live []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "# ") && strings.Contains(l, " = ") && !strings.Contains(l, "Added by") {
			live = append(live, strings.TrimPrefix(l, "# "))
		}
	}
	if len(live) != len(SettingKeys) {
		t.Fatalf("found %d example lines, want %d", len(live), len(SettingKeys))
	}
	if _, err := Load(nil, env(map[string]string{"HOME": home}, map[string]string{defaultFile: strings.Join(live, "\n")}), io.Discard); err != nil {
		t.Fatalf("appended examples are not valid when uncommented: %v", err)
	}
}
