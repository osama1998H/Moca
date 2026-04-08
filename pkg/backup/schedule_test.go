package backup

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// withMockCrontab replaces the package-level readCrontab/writeCrontab with
// in-memory implementations. Call the returned cleanup function to restore
// the originals (typically via defer).
func withMockCrontab(content string) func() {
	var mu sync.Mutex
	current := content

	oldRead := readCrontab
	oldWrite := writeCrontab

	readCrontab = func(_ context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}
	writeCrontab = func(_ context.Context, c string) error {
		mu.Lock()
		defer mu.Unlock()
		current = c
		return nil
	}
	return func() {
		readCrontab = oldRead
		writeCrontab = oldWrite
	}
}

// readMockCrontab reads back the current mock crontab content.
func readMockCrontab(t *testing.T) string {
	t.Helper()
	content, err := readCrontab(context.Background())
	if err != nil {
		t.Fatalf("readMockCrontab: %v", err)
	}
	return content
}

func TestValidateCronExpr(t *testing.T) {
	valid := []string{
		"0 2 * * *",
		"*/5 * * * *",
		"30 3 1 * *",
		"@daily",
		"@hourly",
	}
	for _, expr := range valid {
		t.Run("valid_"+expr, func(t *testing.T) {
			if err := validateCronExpr(expr); err != nil {
				t.Errorf("validateCronExpr(%q) unexpected error: %v", expr, err)
			}
		})
	}

	invalid := []string{
		"not a cron",
		"* * *",
		"",
		"a b c d e",
	}
	for _, expr := range invalid {
		t.Run("invalid_"+expr, func(t *testing.T) {
			if err := validateCronExpr(expr); err == nil {
				t.Errorf("validateCronExpr(%q) expected error, got nil", expr)
			}
		})
	}
}

func TestCronMarker(t *testing.T) {
	got := cronMarker("my-erp")
	want := "# moca-backup:my-erp"
	if got != want {
		t.Errorf("cronMarker(\"my-erp\") = %q, want %q", got, want)
	}
}

func TestCronCommand(t *testing.T) {
	got := cronCommand("/path/to/project")
	want := "cd /path/to/project && moca backup create --compress"
	if got != want {
		t.Errorf("cronCommand = %q, want %q", got, want)
	}
}

func TestInstallCronSchedule_NewEntry(t *testing.T) {
	cleanup := withMockCrontab("")
	defer cleanup()

	ctx := context.Background()
	err := InstallCronSchedule(ctx, "0 2 * * *", "my-erp", "/srv/my-erp")
	if err != nil {
		t.Fatalf("InstallCronSchedule: %v", err)
	}

	content := readMockCrontab(t)
	if !strings.Contains(content, "# moca-backup:my-erp") {
		t.Error("missing marker line")
	}
	if !strings.Contains(content, "0 2 * * * cd /srv/my-erp && moca backup create --compress") {
		t.Errorf("unexpected content:\n%s", content)
	}
}

func TestInstallCronSchedule_ReplaceExisting(t *testing.T) {
	initial := "# some other job\n*/10 * * * * /usr/bin/check-health\n# moca-backup:my-erp\n0 2 * * * cd /old/path && moca backup create --compress\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	err := InstallCronSchedule(ctx, "30 3 * * *", "my-erp", "/new/path")
	if err != nil {
		t.Fatalf("InstallCronSchedule: %v", err)
	}

	content := readMockCrontab(t)
	if !strings.Contains(content, "30 3 * * * cd /new/path && moca backup create --compress") {
		t.Errorf("expected updated entry, got:\n%s", content)
	}
	if strings.Contains(content, "/old/path") {
		t.Error("old entry should have been replaced")
	}
	// Other entries must survive.
	if !strings.Contains(content, "*/10 * * * * /usr/bin/check-health") {
		t.Error("other crontab entries were corrupted")
	}
}

func TestInstallCronSchedule_InvalidExpr(t *testing.T) {
	cleanup := withMockCrontab("")
	defer cleanup()

	ctx := context.Background()
	err := InstallCronSchedule(ctx, "not a cron", "my-erp", "/srv/my-erp")
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
	if !strings.Contains(err.Error(), "invalid cron expression") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRemoveCronSchedule(t *testing.T) {
	initial := "# some other job\n*/10 * * * * /usr/bin/check-health\n# moca-backup:my-erp\n0 2 * * * cd /srv/my-erp && moca backup create --compress\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	err := RemoveCronSchedule(ctx, "my-erp")
	if err != nil {
		t.Fatalf("RemoveCronSchedule: %v", err)
	}

	content := readMockCrontab(t)
	if strings.Contains(content, "moca-backup:my-erp") {
		t.Error("marker should have been removed")
	}
	if strings.Contains(content, "moca backup create") {
		t.Error("command line should have been removed")
	}
	// Other entries must survive.
	if !strings.Contains(content, "*/10 * * * * /usr/bin/check-health") {
		t.Error("other crontab entries were corrupted")
	}
}

func TestRemoveCronSchedule_NotFound(t *testing.T) {
	initial := "# some other job\n*/10 * * * * /usr/bin/check-health\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	err := RemoveCronSchedule(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("RemoveCronSchedule for missing project should return nil, got: %v", err)
	}

	// Crontab should be unchanged.
	content := readMockCrontab(t)
	if !strings.Contains(content, "*/10 * * * * /usr/bin/check-health") {
		t.Error("crontab was modified unexpectedly")
	}
}

func TestDisableCronSchedule(t *testing.T) {
	initial := "# moca-backup:my-erp\n0 2 * * * cd /srv/my-erp && moca backup create --compress\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	err := DisableCronSchedule(ctx, "my-erp")
	if err != nil {
		t.Fatalf("DisableCronSchedule: %v", err)
	}

	content := readMockCrontab(t)
	if !strings.Contains(content, "# moca-backup:my-erp") {
		t.Error("marker should be preserved")
	}
	if !strings.Contains(content, "# DISABLED: 0 2 * * * cd /srv/my-erp && moca backup create --compress") {
		t.Errorf("expected disabled line, got:\n%s", content)
	}
}

func TestEnableCronSchedule(t *testing.T) {
	initial := "# moca-backup:my-erp\n# DISABLED: 0 2 * * * cd /srv/my-erp && moca backup create --compress\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	err := EnableCronSchedule(ctx, "my-erp")
	if err != nil {
		t.Fatalf("EnableCronSchedule: %v", err)
	}

	content := readMockCrontab(t)
	if strings.Contains(content, "# DISABLED:") {
		t.Error("DISABLED prefix should have been removed")
	}
	if !strings.Contains(content, "0 2 * * * cd /srv/my-erp && moca backup create --compress") {
		t.Errorf("expected enabled line, got:\n%s", content)
	}
}

func TestShowSchedule_Installed(t *testing.T) {
	initial := "# moca-backup:my-erp\n0 2 * * * cd /srv/my-erp && moca backup create --compress\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	info, err := ShowSchedule(ctx, "my-erp")
	if err != nil {
		t.Fatalf("ShowSchedule: %v", err)
	}

	if !info.Installed {
		t.Error("expected Installed=true")
	}
	if !info.Enabled {
		t.Error("expected Enabled=true")
	}
	if info.CronExpr != "0 2 * * *" {
		t.Errorf("CronExpr = %q, want %q", info.CronExpr, "0 2 * * *")
	}
	if info.ProjectRoot != "/srv/my-erp" {
		t.Errorf("ProjectRoot = %q, want %q", info.ProjectRoot, "/srv/my-erp")
	}
	if info.ProjectName != "my-erp" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-erp")
	}
}

func TestShowSchedule_Disabled(t *testing.T) {
	initial := "# moca-backup:my-erp\n# DISABLED: 0 2 * * * cd /srv/my-erp && moca backup create --compress\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	info, err := ShowSchedule(ctx, "my-erp")
	if err != nil {
		t.Fatalf("ShowSchedule: %v", err)
	}

	if !info.Installed {
		t.Error("expected Installed=true")
	}
	if info.Enabled {
		t.Error("expected Enabled=false for disabled schedule")
	}
	if info.CronExpr != "0 2 * * *" {
		t.Errorf("CronExpr = %q, want %q", info.CronExpr, "0 2 * * *")
	}
}

func TestShowSchedule_NotInstalled(t *testing.T) {
	cleanup := withMockCrontab("")
	defer cleanup()

	ctx := context.Background()
	info, err := ShowSchedule(ctx, "my-erp")
	if err != nil {
		t.Fatalf("ShowSchedule: %v", err)
	}

	if info.Installed {
		t.Error("expected Installed=false")
	}
	if info.Enabled {
		t.Error("expected Enabled=false when not installed")
	}
	if info.ProjectName != "my-erp" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-erp")
	}
}

func TestInstallCronSchedule_PreservesOtherEntries(t *testing.T) {
	initial := "MAILTO=admin@example.com\n# nightly cleanup\n0 3 * * * /usr/bin/cleanup.sh\n# weekly report\n0 9 * * 1 /usr/bin/report.sh\n"
	cleanup := withMockCrontab(initial)
	defer cleanup()

	ctx := context.Background()
	err := InstallCronSchedule(ctx, "0 2 * * *", "my-erp", "/srv/my-erp")
	if err != nil {
		t.Fatalf("InstallCronSchedule: %v", err)
	}

	content := readMockCrontab(t)

	// All original entries must be present.
	for _, want := range []string{
		"MAILTO=admin@example.com",
		"0 3 * * * /usr/bin/cleanup.sh",
		"0 9 * * 1 /usr/bin/report.sh",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing expected line %q in:\n%s", want, content)
		}
	}

	// New entry must be present.
	if !strings.Contains(content, "# moca-backup:my-erp") {
		t.Error("missing marker")
	}
	if !strings.Contains(content, "0 2 * * * cd /srv/my-erp && moca backup create --compress") {
		t.Error("missing command line")
	}
}
