// Package deploy implements the deployment lifecycle for Moca projects.
// It provides setup, update, rollback, promote, status, and history
// operations backed by YAML-based deployment history in .moca/deployments/.
package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Deployment types.
const (
	TypeSetup    = "setup"
	TypeUpdate   = "update"
	TypeRollback = "rollback"
	TypePromote  = "promote"
)

// Deployment statuses.
const (
	StatusSuccess    = "success"
	StatusFailed     = "failed"
	StatusRolledBack = "rolled_back"
	StatusInProgress = "in_progress"
)

// Process states.
const (
	StateRunning       = "running"
	StateStopped       = "stopped"
	StateFailed        = "failed"
	StateNotConfigured = "not_configured"
	StateUnknown       = "unknown"
)

// DeploymentRecord is a single entry in .moca/deployments/history.yaml.
type DeploymentRecord struct {
	StartedAt    time.Time         `yaml:"started_at"               json:"started_at"`
	CompletedAt  time.Time         `yaml:"completed_at"             json:"completed_at"`
	Apps         map[string]string `yaml:"apps,omitempty"           json:"apps,omitempty"`
	ProcessMgr   string            `yaml:"process_mgr,omitempty"    json:"process_mgr,omitempty"`
	ID           string            `yaml:"id"                       json:"id"`
	Type         string            `yaml:"type"                     json:"type"`
	Status       string            `yaml:"status"                   json:"status"`
	Domain       string            `yaml:"domain,omitempty"         json:"domain,omitempty"`
	ProxyEngine  string            `yaml:"proxy_engine,omitempty"   json:"proxy_engine,omitempty"`
	Error        string            `yaml:"error,omitempty"          json:"error,omitempty"`
	RollbackOf   string            `yaml:"rollback_of,omitempty"    json:"rollback_of,omitempty"`
	PromotedFrom string            `yaml:"promoted_from,omitempty"  json:"promoted_from,omitempty"`
	PromotedTo   string            `yaml:"promoted_to,omitempty"    json:"promoted_to,omitempty"`
	Duration     time.Duration     `yaml:"duration"                 json:"duration"`
}

// DeploymentHistory is the top-level structure for .moca/deployments/history.yaml.
type DeploymentHistory struct {
	Records []DeploymentRecord `yaml:"records" json:"records"`
}

// SetupOptions controls the deploy setup pipeline.
type SetupOptions struct {
	Domain      string
	Email       string
	Proxy       string // "caddy" or "nginx"
	Process     string // "systemd" or "docker"
	Workers     string
	TLS         string // "acme", "custom", "none"
	TLSCert     string
	TLSKey      string
	ProjectRoot string
	Background  int
	Firewall    bool
	Fail2ban    bool
	Logrotate   bool
	DryRun      bool
	Yes         bool
}

// UpdateOptions controls the deploy update pipeline.
type UpdateOptions struct {
	ProjectRoot string
	Apps        []string
	Parallel    int
	NoBackup    bool
	NoMigrate   bool
	NoBuild     bool
	NoRestart   bool
	DryRun      bool
}

// RollbackOptions controls the deploy rollback command.
type RollbackOptions struct {
	DeploymentID string
	ProjectRoot  string
	Step         int
	Force        bool
	NoBackup     bool
}

// PromoteOptions controls the deploy promote command.
type PromoteOptions struct {
	SourceEnv   string
	TargetEnv   string
	ProjectRoot string
	DryRun      bool
	SkipBackup  bool
}

// StatusResult holds the current deployment and process state.
type StatusResult struct {
	CurrentDeployment string        `json:"current_deployment"`
	Processes         []ProcessInfo `json:"processes"`
	Uptime            time.Duration `json:"uptime"`
	SiteCount         int           `json:"site_count"`
}

// ProcessInfo describes the state of a single Moca process.
type ProcessInfo struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Uptime string `json:"uptime,omitempty"`
	PID    int    `json:"pid"`
}

// StepResult describes the outcome of a single pipeline step (used for dry-run
// output and progress reporting).
type StepResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Number      int    `json:"number"`
	Skipped     bool   `json:"skipped,omitempty"`
}

// Commander abstracts shell command execution for testability.
type Commander interface {
	// Run executes a command and returns its combined output.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	// RunWithSudo executes a command with sudo privileges.
	RunWithSudo(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultCommander executes commands via os/exec.
type DefaultCommander struct{}

// Run executes the command directly.
func (d DefaultCommander) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// RunWithSudo prefixes the command with sudo if the current user is not root.
func (d DefaultCommander) RunWithSudo(ctx context.Context, name string, args ...string) ([]byte, error) {
	if os.Getuid() == 0 {
		return d.Run(ctx, name, args...)
	}
	sudoArgs := append([]string{"-n", name}, args...)
	return exec.CommandContext(ctx, "sudo", sudoArgs...).CombinedOutput()
}

// GenerateID creates a deployment ID in format dp_YYYYMMDD_HHMMSS.
func GenerateID() string {
	return generateIDAt(time.Now())
}

func generateIDAt(t time.Time) string {
	return fmt.Sprintf("dp_%s", t.UTC().Format("20060102_150405"))
}
