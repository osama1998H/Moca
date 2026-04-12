//go:build integration

package generate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/dockerutil"
)

// ---------------------------------------------------------------------------
// Systemd
// ---------------------------------------------------------------------------

func TestIntegration_GenerateSystemd(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(true)

	paths, err := GenerateSystemd(cfg, "/opt/moca", SystemdOptions{
		OutputDir: dir,
		User:      "moca",
	})
	if err != nil {
		t.Fatalf("GenerateSystemd: %v", err)
	}

	// At least 5 unit files: server, worker, scheduler, outbox, search-sync + moca.target.
	if len(paths) < 6 {
		t.Fatalf("expected at least 6 files, got %d: %v", len(paths), paths)
	}

	// Validate each .service file contains required systemd sections.
	for _, p := range paths {
		if !strings.HasSuffix(p, ".service") {
			continue
		}

		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(p), err)
		}
		content := string(data)

		for _, section := range []string{"[Unit]", "[Service]", "ExecStart"} {
			if !strings.Contains(content, section) {
				t.Errorf("%s missing %q", filepath.Base(p), section)
			}
		}
	}

	// Verify moca.target exists.
	targetPath := filepath.Join(dir, "moca.target")
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		t.Error("moca.target not generated")
	}
}

// ---------------------------------------------------------------------------
// Docker
// ---------------------------------------------------------------------------

func TestIntegration_GenerateDocker(t *testing.T) {
	// Check if docker compose is available; skip if not.
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	dir := t.TempDir()
	cfg := testConfig(false)

	paths, err := GenerateDocker(cfg, "/opt/moca", DockerOptions{
		OutputDir: dir,
		Profile:   "production",
	})
	if err != nil {
		t.Fatalf("GenerateDocker: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no files generated")
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Fatal("docker-compose.yml not generated")
	}

	// Validate with `docker compose config` — this parses and normalizes the
	// compose file, exiting non-zero on syntax errors.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bin, args := dockerutil.ComposeArgs("-f", composePath, "config")
	validateCmd := exec.CommandContext(ctx, bin, args...)
	out, err := validateCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose config failed:\n%s\nerror: %v", out, err)
	}
}

// ---------------------------------------------------------------------------
// Kubernetes
// ---------------------------------------------------------------------------

func TestIntegration_GenerateK8s(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(true)

	paths, err := GenerateK8s(cfg, "/opt/moca", K8sOptions{
		OutputDir: dir,
		Namespace: "integration-test",
		Replicas:  2,
	})
	if err != nil {
		t.Fatalf("GenerateK8s: %v", err)
	}

	// Multiple YAML files produced.
	if len(paths) < 2 {
		t.Fatalf("expected multiple manifests, got %d", len(paths))
	}

	// Each file must contain apiVersion and kind.
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(p), err)
		}
		content := string(data)

		if !strings.Contains(content, "apiVersion:") {
			t.Errorf("%s missing apiVersion:", filepath.Base(p))
		}
		if !strings.Contains(content, "kind:") {
			t.Errorf("%s missing kind:", filepath.Base(p))
		}
	}

	// Verify at least a Deployment and Service manifest exist.
	foundDeployment := false
	foundService := false
	for _, p := range paths {
		data, _ := os.ReadFile(p)
		content := string(data)
		if strings.Contains(content, "kind: Deployment") {
			foundDeployment = true
		}
		if strings.Contains(content, "kind: Service") {
			foundService = true
		}
	}
	if !foundDeployment {
		t.Error("no Deployment manifest found")
	}
	if !foundService {
		t.Error("no Service manifest found")
	}
}

// ---------------------------------------------------------------------------
// Caddy
// ---------------------------------------------------------------------------

func TestIntegration_GenerateCaddy(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(false)
	out := filepath.Join(dir, "Caddyfile")

	_, err := GenerateCaddy(cfg, "/opt/moca", CaddyOptions{
		OutputPath: out,
		Domain:     "moca.example.com",
	})
	if err != nil {
		t.Fatalf("GenerateCaddy: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	content := string(data)

	// Domain name present.
	if !strings.Contains(content, "moca.example.com") {
		t.Error("Caddyfile missing domain name")
	}

	// reverse_proxy directive.
	if !strings.Contains(content, "reverse_proxy") {
		t.Error("Caddyfile missing reverse_proxy directive")
	}

	// TLS section or email — the test config sets TLS.Email to "admin@example.com".
	hasTLS := strings.Contains(content, "tls") || strings.Contains(content, "admin@example.com")
	if !hasTLS {
		t.Error("Caddyfile missing tls section or email")
	}
}

// ---------------------------------------------------------------------------
// Env
// ---------------------------------------------------------------------------

func TestIntegration_GenerateEnv(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(true)
	out := filepath.Join(dir, ".env")

	_, err := GenerateEnv(cfg, "/opt/moca", EnvOptions{
		OutputPath: out,
		Format:     "dotenv",
	})
	if err != nil {
		t.Fatalf("GenerateEnv: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	content := string(data)

	// Lines match KEY=VALUE pattern (ignoring comments and blank lines).
	kvPattern := regexp.MustCompile(`^[A-Z_]+=.+$`)
	lines := strings.Split(content, "\n")
	kvCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !kvPattern.MatchString(line) {
			t.Errorf("line does not match KEY=VALUE pattern: %q", line)
		}
		kvCount++
	}
	if kvCount == 0 {
		t.Error("no KEY=VALUE lines found in .env")
	}

	// Contains expected keys.
	for _, key := range []string{"MOCA_DB_HOST", "MOCA_REDIS_HOST"} {
		if !strings.Contains(content, key) {
			t.Errorf(".env missing expected key %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Kafka conditional generation
// ---------------------------------------------------------------------------

func TestIntegration_GenerateKafkaConditional(t *testing.T) {
	t.Run("kafka_enabled", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(true)

		paths, err := GenerateSystemd(cfg, "/opt/moca", SystemdOptions{
			OutputDir: dir,
			User:      "moca",
		})
		if err != nil {
			t.Fatalf("GenerateSystemd (kafka enabled): %v", err)
		}

		// Outbox and search-sync units must be present.
		hasOutbox := false
		hasSearchSync := false
		for _, p := range paths {
			base := filepath.Base(p)
			if strings.Contains(base, "outbox") {
				hasOutbox = true
			}
			if strings.Contains(base, "search-sync") {
				hasSearchSync = true
			}
		}
		if !hasOutbox {
			t.Error("with Kafka enabled: outbox unit not generated")
		}
		if !hasSearchSync {
			t.Error("with Kafka enabled: search-sync unit not generated")
		}
	})

	t.Run("kafka_disabled", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(false)

		paths, err := GenerateSystemd(cfg, "/opt/moca", SystemdOptions{
			OutputDir: dir,
			User:      "moca",
		})
		if err != nil {
			t.Fatalf("GenerateSystemd (kafka disabled): %v", err)
		}

		// Outbox and search-sync units must NOT be present.
		for _, p := range paths {
			base := filepath.Base(p)
			if strings.Contains(base, "outbox") {
				t.Error("with Kafka disabled: outbox unit should not be generated")
			}
			if strings.Contains(base, "search-sync") {
				t.Error("with Kafka disabled: search-sync unit should not be generated")
			}
		}

		// Double-check the files do not exist on disk.
		if _, err := os.Stat(filepath.Join(dir, "moca-outbox.service")); err == nil {
			t.Error("with Kafka disabled: moca-outbox.service exists on disk")
		}
		if _, err := os.Stat(filepath.Join(dir, "moca-search-sync.service")); err == nil {
			t.Error("with Kafka disabled: moca-search-sync.service exists on disk")
		}
	})
}
