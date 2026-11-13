package config

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestSourcesAreTrackedPerSetting(t *testing.T) {
	file := map[string]string{defaultFile: "browser = \"fromfile\"\ndownloads = true\ndownload_max_gb = 2\n"}
	vars := map[string]string{"HOME": home, "EARTUBE_DOWNLOADS": "false", "EARTUBE_DELETE_PLAYED": "true"}
	cfg, err := Load([]string{"--download-max-gb=9"}, env(vars, file), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"browser":         "config file",
		"downloads":       "env EARTUBE_DOWNLOADS",  // env beats the file's downloads = true
		"download_max_gb": "flag --download-max-gb", // flag beats the file's 2
		"delete_played":   "env EARTUBE_DELETE_PLAYED",
		"download_dir":    "default",
	}
	for k, w := range want {
		if got := cfg.Sources[k]; got != w {
			t.Errorf("source of %s = %q, want %q", k, got, w)
		}
	}
	if cfg.Path != defaultFile || !cfg.FileExists {
		t.Errorf("path/exists = %q/%v, want %q/true", cfg.Path, cfg.FileExists, defaultFile)
	}
}

func TestEverythingIsDefaultWithNoFile(t *testing.T) {
	cfg, err := Load(nil, env(map[string]string{"HOME": home}, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FileExists {
		t.Error("FileExists should be false")
	}
	for _, k := range SettingKeys {
		if cfg.Sources[k] != "default" {
			t.Errorf("source of %s = %q, want default", k, cfg.Sources[k])
		}
	}
}

func TestExplicitDefaultValueInTheFileStillCountsAsFromTheFile(t *testing.T) {
	file := map[string]string{defaultFile: "download_max_gb = 5\n"} // same as the default, but set on purpose
	cfg, _ := Load(nil, env(map[string]string{"HOME": home}, file), io.Discard)
	if cfg.Sources["download_max_gb"] != "config file" {
		t.Errorf("source = %q, want config file", cfg.Sources["download_max_gb"])
	}
}

func TestRows(t *testing.T) {
	cfg, _ := Load([]string{"--browser=brave+gnomekeyring", "--download-max-gb=0"}, env(map[string]string{"HOME": home}, nil), io.Discard)
	rows := cfg.Rows("/data/eartube/downloads")
	if len(rows) != len(SettingKeys) {
		t.Fatalf("got %d rows, want %d", len(rows), len(SettingKeys))
	}
	got := map[string]Row{}
	for i, r := range rows {
		if r.Key != SettingKeys[i] {
			t.Errorf("row %d is %s, want %s (the documented order)", i, r.Key, SettingKeys[i])
		}
		got[r.Key] = r
	}
	check := func(key, value, source string) {
		t.Helper()
		if r := got[key]; r.Value != value || r.Source != source {
			t.Errorf("%s = %q from %q, want %q from %q", key, r.Value, r.Source, value, source)
		}
	}
	check("browser", "brave+gnomekeyring", "flag --browser")
	check("downloads", "off", "default")
	check("download_max_gb", "unlimited", "flag --download-max-gb")
	check("delete_played", "off", "default")
	check("download_dir", "/data/eartube/downloads", "default") // shows where it really goes

	plain, _ := Load(nil, env(map[string]string{"HOME": home}, nil), io.Discard)
	for _, r := range plain.Rows("/d") {
		switch r.Key {
		case "browser":
			if r.Value != "(none: search only)" {
				t.Errorf("empty browser shown as %q", r.Value)
			}
		case "download_max_gb":
			if r.Value != "5 GiB" {
				t.Errorf("default cap shown as %q", r.Value)
			}
		}
	}
	if (Config{}).Rows("/d")[0].Source != "default" {
		t.Error("a zero Config (no Sources map) must not panic or show a blank source")
	}
}

// --- the starter file -----------------------------------------------------------

func TestTemplateIsAValidConfigThatChangesNothing(t *testing.T) {
	cfg, err := Load(nil, env(map[string]string{"HOME": home}, map[string]string{defaultFile: Template()}), io.Discard)
	if err != nil {
		t.Fatalf("the starter file does not load: %v", err)
	}
	if cfg.Browser != "" || cfg.Downloads || cfg.DeletePlayed || cfg.DownloadMaxGB != 5 || cfg.DownloadDir != "" {
		t.Errorf("the starter file changed a setting: %+v", cfg)
	}
	for _, k := range SettingKeys {
		if cfg.Sources[k] != "default" {
			t.Errorf("%s reads as %q; everything in the starter file must be commented out", k, cfg.Sources[k])
		}
	}
}

// Uncommenting every example line must still be valid: no typo'd key, no wrong type.
func TestEveryExampleLineInTheTemplateIsValidWhenUncommented(t *testing.T) {
	line := regexp.MustCompile(`(?m)^# ([a-z_]+ = .*)$`)
	live := line.ReplaceAllString(Template(), "$1")
	if live == Template() {
		t.Fatal("found no commented example lines")
	}
	cfg, err := Load(nil, env(map[string]string{"HOME": home}, map[string]string{defaultFile: live}), io.Discard)
	if err != nil {
		t.Fatalf("uncommented template does not load (a typo or wrong type in an example): %v\n%s", err, live)
	}
	for _, k := range SettingKeys {
		if cfg.Sources[k] != "config file" {
			t.Errorf("example for %s was not picked up from the file (source %q)", k, cfg.Sources[k])
		}
	}
}

// Adding a setting to Config without documenting it here would leave users
// guessing, so every settable field must be in SettingKeys and in the template.
func TestTemplateAndSettingKeysCoverEveryConfigField(t *testing.T) {
	var fields []string
	rt := reflect.TypeOf(Config{})
	for i := 0; i < rt.NumField(); i++ {
		if tag := rt.Field(i).Tag.Get("toml"); tag != "" && tag != "-" {
			fields = append(fields, tag)
		}
	}
	if len(fields) != len(SettingKeys) {
		t.Errorf("Config has settings %v but SettingKeys is %v", fields, SettingKeys)
	}
	for _, f := range fields {
		found := false
		for _, k := range SettingKeys {
			found = found || k == f
		}
		if !found {
			t.Errorf("%q is a Config setting missing from SettingKeys", f)
		}
		if !strings.Contains(Template(), "# "+f+" = ") {
			t.Errorf("%q is not documented in Template()", f)
		}
	}
}

func TestEnsureFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "er", "config.toml") // parent directories do not exist yet

	created, err := EnsureFile(path)
	if err != nil || !created {
		t.Fatalf("EnsureFile = %v, %v; want it to create the file", created, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != Template() {
		t.Fatalf("file contents wrong (err %v)", err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", st.Mode().Perm())
	}

	// Never clobber what the user has written.
	mine := "browser = \"firefox\"\n# my own notes\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err = EnsureFile(path)
	if err != nil || created {
		t.Fatalf("second EnsureFile = %v, %v; want no-op", created, err)
	}
	if got, _ := os.ReadFile(path); string(got) != mine {
		t.Fatalf("EnsureFile overwrote the user's file:\n%s", got)
	}
}

func TestEnsureFileReportsAnUnwritableLocation(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "afile")
	os.WriteFile(blocker, nil, 0o644)
	if _, err := EnsureFile(filepath.Join(blocker, "config.toml")); err == nil {
		t.Fatal("expected an error when the parent is a file")
	}
}

func TestCheck(t *testing.T) {
	good := env(map[string]string{"HOME": home}, map[string]string{defaultFile: "downloads = true\n"})
	if err := Check(good); err != nil {
		t.Errorf("a valid config failed Check: %v", err)
	}
	for name, e := range map[string]Env{
		"typo in a key":  env(map[string]string{"HOME": home}, map[string]string{defaultFile: "downlods = true\n"}),
		"wrong type":     env(map[string]string{"HOME": home}, map[string]string{defaultFile: "downloads = \"yes\"\n"}),
		"invalid syntax": env(map[string]string{"HOME": home}, map[string]string{defaultFile: "downloads = \n"}),
		"negative cap":   env(map[string]string{"HOME": home}, map[string]string{defaultFile: "download_max_gb = -2\n"}),
	} {
		if err := Check(e); err == nil {
			t.Errorf("%s: Check passed a broken config", name)
		}
	}
	if err := Check(env(map[string]string{"HOME": home}, nil)); err != nil {
		t.Errorf("no config file at all must be fine: %v", err)
	}
}

func TestEveryFlagIsReportedAsTheSource(t *testing.T) {
	args := []string{"--browser=b", "--downloads", "--download-max-gb=3", "--delete-played", "--download-dir=/d"}
	cfg, err := Load(args, env(map[string]string{"HOME": home}, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"browser": "flag --browser", "downloads": "flag --downloads", "download_max_gb": "flag --download-max-gb",
		"delete_played": "flag --delete-played", "download_dir": "flag --download-dir",
	}
	for k, w := range want {
		if cfg.Sources[k] != w {
			t.Errorf("source of %s = %q, want %q", k, cfg.Sources[k], w)
		}
	}
}

func TestEveryEnvVarIsReportedAsTheSource(t *testing.T) {
	vars := map[string]string{"HOME": home, "EARTUBE_BROWSER": "b", "EARTUBE_DOWNLOADS": "1", "EARTUBE_DOWNLOAD_MAX_GB": "3",
		"EARTUBE_DELETE_PLAYED": "1", "EARTUBE_DOWNLOAD_DIR": "/d", "EARTUBE_SUBSCRIPTIONS_COUNT": "7"}
	cfg, err := Load(nil, env(vars, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"browser": "env EARTUBE_BROWSER", "downloads": "env EARTUBE_DOWNLOADS", "download_max_gb": "env EARTUBE_DOWNLOAD_MAX_GB",
		"delete_played": "env EARTUBE_DELETE_PLAYED", "download_dir": "env EARTUBE_DOWNLOAD_DIR",
		"subscriptions_count": "env EARTUBE_SUBSCRIPTIONS_COUNT",
	}
	for k, w := range want {
		if cfg.Sources[k] != w {
			t.Errorf("source of %s = %q, want %q", k, cfg.Sources[k], w)
		}
	}
}

func TestSubscriptionsCount(t *testing.T) {
	load := func(args []string, vars, file map[string]string) (Config, error) {
		v := map[string]string{"HOME": home}
		for k, x := range vars {
			v[k] = x
		}
		return Load(args, env(v, file), io.Discard)
	}
	cfg, err := load(nil, nil, nil)
	if err != nil || cfg.SubscriptionsCount != DefaultSubscriptionsCount {
		t.Fatalf("default = %d, %v; want %d", cfg.SubscriptionsCount, err, DefaultSubscriptionsCount)
	}
	file := map[string]string{defaultFile: "subscriptions_count = 20\n"}
	envVar := map[string]string{"EARTUBE_SUBSCRIPTIONS_COUNT": "30"}
	for _, c := range []struct {
		name       string
		args       []string
		vars, file map[string]string
		want       int
		source     string
	}{
		{"file", nil, nil, file, 20, "config file"},
		{"env beats file", nil, envVar, file, 30, "env EARTUBE_SUBSCRIPTIONS_COUNT"},
		{"flag beats env", []string{"--subscriptions-count=40"}, envVar, file, 40, "flag --subscriptions-count"},
	} {
		cfg, err := load(c.args, c.vars, c.file)
		if err != nil || cfg.SubscriptionsCount != c.want || cfg.Sources["subscriptions_count"] != c.source {
			t.Errorf("%s: got %d from %q (%v), want %d from %q", c.name, cfg.SubscriptionsCount,
				cfg.Sources["subscriptions_count"], err, c.want, c.source)
		}
	}
	for _, bad := range []struct {
		args []string
		vars map[string]string
	}{
		{[]string{"--subscriptions-count=0"}, nil},
		{nil, map[string]string{"EARTUBE_SUBSCRIPTIONS_COUNT": "-3"}},
		{nil, map[string]string{"EARTUBE_SUBSCRIPTIONS_COUNT": "lots"}},
	} {
		if _, err := load(bad.args, bad.vars, nil); err == nil {
			t.Errorf("%v %v: accepted, want an error", bad.args, bad.vars)
		}
	}
}
