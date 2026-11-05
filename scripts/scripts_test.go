// Package scripts tests the shell scripts in this directory by running them for
// real, with the network, notifier and systemd replaced by fakes.
package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- helpers -------------------------------------------------------------------

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// sourceRepo makes a real git repository on branch main with one commit, so the
// script's clone step is exercised for real. It returns the path and the commit.
func sourceRepo(t *testing.T) (path, sha string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "source")
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", path, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-q", "-b", "main")
	write(t, filepath.Join(path, "README"), "hello\n", 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "first")
	return path, run("rev-parse", "HEAD")
}

type check struct {
	t     *testing.T
	state string // EARTUBE_CHECK_STATE_DIR
	tmp   string // TMPDIR: must be empty again afterwards
	notes string // the fake notifier appends one line per call here
	env   map[string]string
}

func newCheck(t *testing.T) *check {
	t.Helper()
	src, _ := sourceRepo(t)
	dir := t.TempDir()
	c := &check{t: t, state: filepath.Join(dir, "state"), tmp: filepath.Join(dir, "tmp"), notes: filepath.Join(dir, "notifications")}
	os.MkdirAll(c.tmp, 0o755)
	notifier := filepath.Join(dir, "fake-notify")
	write(t, notifier, "#!/bin/sh\necho \"$*\" >> \""+c.notes+"\"\n", 0o755)
	c.env = map[string]string{
		"HOME":                        dir,
		"TMPDIR":                      c.tmp,
		"EARTUBE_CHECK_STATE_DIR":     c.state,
		"EARTUBE_CHECK_REPO_URL":      src,
		"EARTUBE_CHECK_BRANCH":        "main",
		"EARTUBE_CHECK_NOTIFY":        notifier,
		"EARTUBE_CHECK_ONLINE_CMD":    "true",
		"EARTUBE_CHECK_PREFLIGHT_CMD": "true",
		"EARTUBE_CHECK_TEST_CMD":      "true",
	}
	return c
}

func (c *check) run() (exit int) {
	c.t.Helper()
	cmd := exec.Command("bash", "network-check.sh")
	cmd.Env = os.Environ()
	for k, v := range c.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	if err != nil {
		c.t.Fatalf("could not run the script: %v\n%s", err, out.String())
	}
	return 0
}

func (c *check) notifications() []string {
	if !exists(c.notes) {
		return nil
	}
	return strings.Split(strings.TrimSpace(read(c.t, c.notes)), "\n")
}

func (c *check) result() string { return read(c.t, filepath.Join(c.state, "last-result")) }
func (c *check) log() string    { return read(c.t, filepath.Join(c.state, "network-check.log")) }

func (c *check) assertTempCleanedUp() {
	c.t.Helper()
	if left, _ := os.ReadDir(c.tmp); len(left) != 0 {
		c.t.Errorf("the script left %d item(s) in its temp directory", len(left))
	}
}

// --- network-check.sh ------------------------------------------------------------

func TestPassingRunIsSilent(t *testing.T) {
	c := newCheck(t)
	if code := c.run(); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if n := c.notifications(); n != nil {
		t.Errorf("notified on success: %v", n)
	}
	if r := c.result(); !strings.Contains(r, "PASS") || !strings.Contains(r, "main@") {
		t.Errorf("last-result = %q, want a PASS line naming the commit", r)
	}
	if l := c.log(); !strings.Contains(l, "network check starting") || !strings.Contains(l, "testing main@") {
		t.Errorf("log is missing the start or the commit:\n%s", l)
	}
	c.assertTempCleanedUp()
}

func TestTestsRunInAFreshCloneOfThePublishedBranch(t *testing.T) {
	c := newCheck(t)
	_, sha := func() (string, string) { // the repo newCheck made: find its HEAD
		out, err := exec.Command("git", "-C", c.env["EARTUBE_CHECK_REPO_URL"], "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return "", strings.TrimSpace(string(out))
	}()
	marker := filepath.Join(c.state, "marker")
	c.env["MARKER"] = marker
	c.env["EARTUBE_BROWSER"] = "brave+gnomekeyring"
	c.env["EARTUBE_CHECK_TEST_CMD"] = `{ pwd; git rev-parse HEAD; test -f README && echo has-readme; echo "browser=$EARTUBE_BROWSER"; } > "$MARKER"`
	if code := c.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := read(t, marker)
	if strings.Contains(got, c.env["EARTUBE_CHECK_REPO_URL"]+"\n") {
		t.Errorf("tests ran inside the source repository itself:\n%s", got)
	}
	for _, want := range []string{sha, "has-readme", "browser=brave+gnomekeyring"} {
		if !strings.Contains(got, want) {
			t.Errorf("test environment missing %q:\n%s", want, got)
		}
	}
	c.assertTempCleanedUp()
}

func TestFailingTestsNotifyWithTheirNamesAndKeepTheOutput(t *testing.T) {
	c := newCheck(t)
	c.env["EARTUBE_CHECK_TEST_CMD"] = `printf -- '--- FAIL: TestSearch (1.2s)\n--- FAIL: TestPublicPlaylist (0.4s)\n--- FAIL: TestSearch (0.0s)\nFAIL\n'; exit 1`
	if code := c.run(); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	n := c.notifications()
	if len(n) != 1 {
		t.Fatalf("notifications = %v, want exactly one", n)
	}
	for _, want := range []string{"FAILED", "TestSearch", "TestPublicPlaylist", "last-failure.log"} {
		if !strings.Contains(n[0], want) {
			t.Errorf("notification missing %q: %s", want, n[0])
		}
	}
	if strings.Count(n[0], "TestSearch") != 1 {
		t.Errorf("a test failing twice was listed twice: %s", n[0])
	}
	if !strings.Contains(read(t, filepath.Join(c.state, "last-failure.log")), "--- FAIL: TestSearch") {
		t.Error("the full output of the failed run was not kept")
	}
	if !strings.Contains(c.result(), "FAIL") {
		t.Errorf("last-result = %q", c.result())
	}
	c.assertTempCleanedUp()
}

func TestAFailureWithNoTestNamesStillSaysSomethingUseful(t *testing.T) {
	c := newCheck(t)
	c.env["EARTUBE_CHECK_TEST_CMD"] = `echo "build failed: undefined: Foo"; exit 2`
	if code := c.run(); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if n := c.notifications(); len(n) != 1 || !strings.Contains(n[0], "the test run itself failed") {
		t.Fatalf("notifications = %v", n)
	}
}

func TestAMissingToolSaysToReinstallRatherThanBlamingTheTests(t *testing.T) {
	c := newCheck(t)
	c.env["EARTUBE_CHECK_TEST_CMD"] = `definitely-not-installed-tool test ./...`
	if code := c.run(); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	n := c.notifications()
	if len(n) != 1 || !strings.Contains(n[0], "could not run") || !strings.Contains(n[0], "install-network-check.sh") {
		t.Fatalf("notifications = %v, want one telling the user to re-run the installer", n)
	}
	if strings.Contains(n[0], "FAILED") {
		t.Errorf("a missing tool was reported as failing tests: %s", n[0])
	}
}

func TestOfflineIsSkippedQuietly(t *testing.T) {
	c := newCheck(t)
	marker := filepath.Join(c.state, "tests-ran")
	c.env["EARTUBE_CHECK_ONLINE_CMD"] = "false"
	c.env["MARKER"] = marker
	c.env["EARTUBE_CHECK_TEST_CMD"] = `touch "$MARKER"`
	if code := c.run(); code != 0 {
		t.Fatalf("exit %d; being offline is not a failure", code)
	}
	if n := c.notifications(); n != nil {
		t.Errorf("notified although only the connection was down: %v", n)
	}
	if exists(marker) {
		t.Error("tests ran with no connection")
	}
	if r := c.result(); !strings.Contains(r, "SKIPPED") {
		t.Errorf("last-result = %q, want SKIPPED", r)
	}
}

func TestAnAskForSignInIsReportedAsItsOwnThingAndNothingIsTested(t *testing.T) {
	c := newCheck(t)
	marker := filepath.Join(c.state, "tests-ran")
	c.env["MARKER"] = marker
	c.env["EARTUBE_CHECK_PREFLIGHT_CMD"] = `echo "ERROR: [youtube] x: Sign in to confirm you’re not a bot." >&2; exit 1`
	c.env["EARTUBE_CHECK_TEST_CMD"] = `touch "$MARKER"`
	if code := c.run(); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	n := c.notifications()
	if len(n) != 1 || !strings.Contains(n[0], "wants a sign-in") || !strings.Contains(n[0], "says nothing about the code") {
		t.Fatalf("notifications = %v, want one saying YouTube wants a sign-in and that this says nothing about the code", n)
	}
	if exists(marker) {
		t.Error("tests ran although YouTube was refusing requests")
	}
	if !strings.Contains(c.result(), "BLOCKED") {
		t.Errorf("last-result = %q", c.result())
	}
}

func TestOtherPreflightTroubleStillRunsTheTests(t *testing.T) {
	c := newCheck(t)
	marker := filepath.Join(c.state, "tests-ran")
	c.env["MARKER"] = marker
	c.env["EARTUBE_CHECK_PREFLIGHT_CMD"] = `echo "ERROR: unable to download webpage: timed out" >&2; exit 1`
	c.env["EARTUBE_CHECK_TEST_CMD"] = `touch "$MARKER"`
	if code := c.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !exists(marker) {
		t.Error("a non-bot-check preflight problem must not stop the tests; the real failure should be reported by them")
	}
	if !strings.Contains(c.log(), "preflight could not resolve") {
		t.Errorf("the preflight problem was not logged:\n%s", c.log())
	}
}

func TestAnUnreachableRepositoryIsReported(t *testing.T) {
	c := newCheck(t)
	marker := filepath.Join(c.state, "tests-ran")
	c.env["MARKER"] = marker
	c.env["EARTUBE_CHECK_REPO_URL"] = filepath.Join(t.TempDir(), "does-not-exist")
	c.env["EARTUBE_CHECK_TEST_CMD"] = `touch "$MARKER"`
	if code := c.run(); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if n := c.notifications(); len(n) != 1 || !strings.Contains(n[0], "could not run") {
		t.Fatalf("notifications = %v", n)
	}
	if exists(marker) {
		t.Error("tests ran without a checkout")
	}
	c.assertTempCleanedUp()
}

func TestAMissingBranchIsReported(t *testing.T) {
	c := newCheck(t)
	c.env["EARTUBE_CHECK_BRANCH"] = "no-such-branch"
	if code := c.run(); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if n := c.notifications(); len(n) != 1 {
		t.Fatalf("notifications = %v", n)
	}
}

func TestAMissingNotifierDoesNotCrashTheCheck(t *testing.T) {
	c := newCheck(t)
	c.env["EARTUBE_CHECK_NOTIFY"] = "definitely-not-installed"
	c.env["EARTUBE_CHECK_TEST_CMD"] = `echo '--- FAIL: TestX (0.1s)'; exit 1`
	if code := c.run(); code != 1 {
		t.Fatalf("exit %d, want 1 (a failure, not a crash)", code)
	}
	if !strings.Contains(c.log(), "notification unavailable") {
		t.Errorf("the missing notifier was not logged:\n%s", c.log())
	}
	if !strings.Contains(c.result(), "FAIL") {
		t.Errorf("last-result = %q", c.result())
	}
}

func TestTheLogIsTrimmedButKeepsTheLatestRun(t *testing.T) {
	c := newCheck(t)
	os.MkdirAll(c.state, 0o755)
	write(t, filepath.Join(c.state, "network-check.log"), strings.Repeat("old line of history\n", 20000), 0o644) // ~400 KB
	if code := c.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	st, _ := os.Stat(filepath.Join(c.state, "network-check.log"))
	if st.Size() > 150000 {
		t.Errorf("log is %d bytes, should have been trimmed to about 100 KB plus the new run", st.Size())
	}
	if !strings.Contains(c.log(), "PASS") {
		t.Error("the newest run's result was lost in trimming")
	}
}

func TestEachRunAppendsToTheLog(t *testing.T) {
	c := newCheck(t)
	c.run()
	c.run()
	if n := strings.Count(c.log(), "network check starting"); n != 2 {
		t.Errorf("log has %d run(s), want 2", n)
	}
}

// --- install-network-check.sh ---------------------------------------------------------

type install struct {
	t       *testing.T
	home    string
	bin     string // fake tools come first on PATH
	goroot  string
	sysctl  string // the fake systemctl appends its arguments here
	cfgPath string
}

func newInstall(t *testing.T) *install {
	t.Helper()
	dir := t.TempDir()
	i := &install{t: t, home: filepath.Join(dir, "home"), bin: filepath.Join(dir, "bin"), goroot: filepath.Join(dir, "goroot"), sysctl: filepath.Join(dir, "systemctl.log")}
	i.cfgPath = filepath.Join(i.home, ".config", "eartube", "config.toml")
	write(t, filepath.Join(i.bin, "systemctl"), "#!/bin/sh\necho \"$*\" >> \""+i.sysctl+"\"\n", 0o755)
	// "go" is a shim in the real world: it lives elsewhere, and GOROOT/bin/go is the real one.
	write(t, filepath.Join(i.goroot, "bin", "go"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(i.bin, "go"), "#!/bin/sh\n[ \"$1 $2\" = \"env GOROOT\" ] && echo \""+i.goroot+"\"\n", 0o755)
	return i
}

func (i *install) run(args ...string) (string, int) {
	i.t.Helper()
	cmd := exec.Command("bash", append([]string{"install-network-check.sh"}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+i.home, "XDG_CONFIG_HOME=", "XDG_STATE_HOME=", "PATH="+i.bin+":"+os.Getenv("PATH"))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode()
	}
	if err != nil {
		i.t.Fatalf("could not run the installer: %v", err)
	}
	return out.String(), 0
}

func (i *install) unit(name string) string {
	return filepath.Join(i.home, ".config", "systemd", "user", name)
}
func (i *install) installedScript() string {
	return filepath.Join(i.home, ".local", "share", "eartube", "network-check.sh")
}

func TestInstallWritesUnitsAndEnablesTheTimer(t *testing.T) {
	i := newInstall(t)
	write(t, i.cfgPath, "# note\nbrowser = \"brave+gnomekeyring\"\n", 0o644)
	out, code := i.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}

	svc := read(t, i.unit("eartube-network-check.service"))
	for _, want := range []string{
		"Type=oneshot",
		"ExecStart=" + i.installedScript(),
		"Environment=EARTUBE_BROWSER=brave+gnomekeyring",
		"Environment=PATH=" + i.goroot + "/bin:", // the real Go, not the shim
		"/usr/bin:/bin",
	} {
		if !strings.Contains(svc, want) {
			t.Errorf("service missing %q:\n%s", want, svc)
		}
	}
	timer := read(t, i.unit("eartube-network-check.timer"))
	for _, want := range []string{"OnCalendar=Mon *-*-*", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer missing %q:\n%s", want, timer)
		}
	}
	if got, want := read(t, i.installedScript()), read(t, "network-check.sh"); got != want {
		t.Error("the installed script differs from scripts/network-check.sh")
	}
	if st, _ := os.Stat(i.installedScript()); st.Mode().Perm()&0o100 == 0 {
		t.Error("the installed script is not executable")
	}
	log := read(t, i.sysctl)
	for _, want := range []string{"--user daemon-reload", "--user enable --now eartube-network-check.timer"} {
		if !strings.Contains(log, want) {
			t.Errorf("systemctl was never asked %q; calls:\n%s", want, log)
		}
	}
	if !strings.Contains(out, "brave+gnomekeyring") || !strings.Contains(out, "--uninstall") {
		t.Errorf("the summary should say which browser is used and how to remove it:\n%s", out)
	}
}

func TestInstallLeavesTheRealSystemAlone(t *testing.T) {
	i := newInstall(t)
	if _, code := i.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	// Only the throwaway HOME was written to.
	real, _ := os.UserHomeDir()
	if real != "" && real != i.home {
		for _, f := range []string{".config/systemd/user/eartube-network-check.timer", ".local/share/eartube/network-check.sh"} {
			if st, err := os.Stat(filepath.Join(real, f)); err == nil && st.ModTime().After(time.Now().Add(-time.Minute)) {
				t.Errorf("%s in the real home directory was touched", f)
			}
		}
	}
}

// If the versioned Go directory vanishes (an upgrade), the shim directory it came
// from must still be on the service's PATH, after the real one.
func TestInstallKeepsTheShimDirectoryAsAFallback(t *testing.T) {
	i := newInstall(t)
	if _, code := i.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	svc := read(t, i.unit("eartube-network-check.service"))
	real, shim := i.goroot+"/bin", i.bin
	ir, is := strings.Index(svc, real), strings.Index(svc, shim)
	if ir < 0 || is < 0 || ir > is {
		t.Errorf("want the real Go (%s) then the shim dir (%s) on PATH:\n%s", real, shim, svc)
	}
}

func TestInstallWithoutAConfiguredBrowserSetsNone(t *testing.T) {
	i := newInstall(t) // no config file
	out, code := i.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if svc := read(t, i.unit("eartube-network-check.service")); !strings.Contains(svc, "Environment=EARTUBE_BROWSER=\n") {
		t.Errorf("want an empty EARTUBE_BROWSER:\n%s", svc)
	}
	if !strings.Contains(out, "none") {
		t.Errorf("the summary should say no browser is used:\n%s", out)
	}
}

func TestInstallBrowserFlagOverridesTheConfig(t *testing.T) {
	i := newInstall(t)
	write(t, i.cfgPath, "browser = \"firefox\"\n", 0o644)
	if _, code := i.run("--browser", "chromium"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if svc := read(t, i.unit("eartube-network-check.service")); !strings.Contains(svc, "EARTUBE_BROWSER=chromium\n") {
		t.Errorf("flag ignored:\n%s", svc)
	}
	if _, code := i.run("--browser"); code != 0 { // a trailing --browser means "none"; it must not crash
		t.Fatalf("a trailing --browser crashed the installer (exit %d)", code)
	}
	if svc := read(t, i.unit("eartube-network-check.service")); !strings.Contains(svc, "EARTUBE_BROWSER=\n") {
		t.Errorf("an empty --browser should mean no browser:\n%s", svc)
	}
}

func TestInstallIgnoresCommentedOutBrowserLines(t *testing.T) {
	i := newInstall(t)
	write(t, i.cfgPath, "# browser = \"commented\"\n  browser = \"real\"  \n", 0o644)
	if _, code := i.run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if svc := read(t, i.unit("eartube-network-check.service")); !strings.Contains(svc, "EARTUBE_BROWSER=real\n") {
		t.Errorf("picked the wrong line:\n%s", svc)
	}
}

func TestInstallTwiceIsHarmless(t *testing.T) {
	i := newInstall(t)
	i.run()
	first := read(t, i.unit("eartube-network-check.service")) + read(t, i.unit("eartube-network-check.timer"))
	if _, code := i.run(); code != 0 {
		t.Fatalf("second install exit %d", code)
	}
	second := read(t, i.unit("eartube-network-check.service")) + read(t, i.unit("eartube-network-check.timer"))
	if first != second {
		t.Error("installing twice changed the units")
	}
}

func TestUninstallRemovesTheTimerButKeepsTheHistory(t *testing.T) {
	i := newInstall(t)
	i.run()
	state := filepath.Join(i.home, ".local", "state", "eartube")
	write(t, filepath.Join(state, "network-check.log"), "history\n", 0o644)
	os.Truncate(i.sysctl, 0)

	out, code := i.run("--uninstall")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, f := range []string{i.unit("eartube-network-check.service"), i.unit("eartube-network-check.timer"), i.installedScript()} {
		if exists(f) {
			t.Errorf("%s was not removed", f)
		}
	}
	if !exists(filepath.Join(state, "network-check.log")) {
		t.Error("uninstall deleted the log")
	}
	log := read(t, i.sysctl)
	if !strings.Contains(log, "--user disable --now eartube-network-check.timer") || !strings.Contains(log, "--user daemon-reload") {
		t.Errorf("systemctl calls:\n%s", log)
	}
	if _, code := i.run("--uninstall"); code != 0 { // and removing twice is fine
		t.Errorf("second uninstall exit %d", code)
	}
}

func TestInstallWithoutSystemdSaysSoAndWritesNothing(t *testing.T) {
	i := newInstall(t)
	// A PATH with the basic tools the script needs but no systemctl.
	tools := filepath.Join(t.TempDir(), "tools")
	os.MkdirAll(tools, 0o755)
	for _, name := range []string{"dirname", "sed", "head", "mkdir", "install", "cat", "rm"} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s not available", name)
		}
		os.Symlink(p, filepath.Join(tools, name))
	}
	cmd := exec.Command("bash", "install-network-check.sh")
	cmd.Env = []string{"HOME=" + i.home, "PATH=" + tools}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected a failure without systemctl:\n%s", out)
	}
	if !strings.Contains(string(out), "systemctl not found") {
		t.Errorf("unhelpful message: %s", out)
	}
	if exists(i.unit("eartube-network-check.timer")) || exists(i.installedScript()) {
		t.Error("files were written although the install could not complete")
	}
}

func TestInstallerRejectsUnknownOptions(t *testing.T) {
	i := newInstall(t)
	out, code := i.run("--frobnicate")
	if code == 0 || !strings.Contains(out, "unknown option") {
		t.Fatalf("exit %d: %s", code, out)
	}
	if exists(i.unit("eartube-network-check.timer")) {
		t.Error("something was installed despite the bad option")
	}
}

func TestBothScriptsAreSyntacticallyValid(t *testing.T) {
	for _, f := range []string{"network-check.sh", "install-network-check.sh"} {
		if out, err := exec.Command("bash", "-n", f).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", f, err, out)
		}
	}
}
