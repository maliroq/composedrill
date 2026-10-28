package model

import "time"

type Mount struct {
	Type     string `json:"type"`
	Source   string `json:"source,omitempty"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type Service struct {
	Name             string   `json:"name"`
	Image            string   `json:"image,omitempty"`
	Build            bool     `json:"build,omitempty"`
	Mounts           []Mount  `json:"mounts,omitempty"`
	Ports            []string `json:"ports,omitempty"`
	Healthcheck      bool     `json:"healthcheck"`
	EnvironmentRefs  []string `json:"environment_refs,omitempty"`
	EnvFiles         []string `json:"env_files,omitempty"`
	Secrets          []string `json:"secrets,omitempty"`
	DependsOn        []string `json:"depends_on,omitempty"`
	Profiles         []string `json:"profiles,omitempty"`
	Database         string   `json:"database,omitempty"`
	DatabaseStrategy string   `json:"database_strategy,omitempty"`
	RiskFlags        []string `json:"risk_flags,omitempty"`
}

type Project struct {
	Name               string            `json:"name"`
	Dir                string            `json:"dir"`
	ComposeFile        string            `json:"compose_file"`
	ComposeSHA256      string            `json:"compose_sha256"`
	GitRevision        string            `json:"git_revision,omitempty"`
	Services           []Service         `json:"services"`
	NamedVolumes       []string          `json:"named_volumes,omitempty"`
	BindMounts         []Mount           `json:"bind_mounts,omitempty"`
	Secrets            []string          `json:"secrets,omitempty"`
	Warnings           []string          `json:"warnings,omitempty"`
	UnsupportedYAML    []string          `json:"unsupported_yaml,omitempty"`
	TopLevelVolumeRisk map[string]string `json:"top_level_volume_risk,omitempty"`
}

type Finding struct {
	Severity string `json:"severity"`
	Project  string `json:"project,omitempty"`
	Service  string `json:"service,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Remedy   string `json:"remedy,omitempty"`
}

type ScanResult struct {
	Root             string    `json:"root"`
	GeneratedAt      time.Time `json:"generated_at"`
	Projects         []Project `json:"projects"`
	Findings         []Finding `json:"findings"`
	ReadinessScore   int       `json:"recovery_readiness_score"`
	ServiceCount     int       `json:"service_count"`
	NamedVolumeCount int       `json:"named_volume_count"`
	BindMountCount   int       `json:"bind_mount_count"`
	DatabaseCount    int       `json:"database_count"`
	HealthcheckCount int       `json:"healthcheck_count"`
	UnpinnedImages   int       `json:"unpinned_images"`
}

type Check struct {
	Name           string   `json:"name"`
	Service        string   `json:"service"`
	Type           string   `json:"type"`
	Port           int      `json:"port,omitempty"`
	Path           string   `json:"path,omitempty"`
	ExpectedStatus int      `json:"expected_status,omitempty"`
	ExpectContains string   `json:"expect_contains,omitempty"`
	Host           string   `json:"host,omitempty"`
	Command        []string `json:"command,omitempty"`
	File           string   `json:"file,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type UserConfig struct {
	Version int     `json:"version"`
	Checks  []Check `json:"checks,omitempty"`
}

type BackupEntry struct {
	Kind     string `json:"kind"`
	Project  string `json:"project,omitempty"`
	Service  string `json:"service,omitempty"`
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	Volume   string `json:"volume,omitempty"`
	Engine   string `json:"engine,omitempty"`
	Artifact string `json:"artifact"`
	SHA256   string `json:"sha256"`
	Strategy string `json:"strategy,omitempty"`
}

type BackupManifest struct {
	Format        string        `json:"format"`
	Version       int           `json:"version"`
	CreatedAt     time.Time     `json:"created_at"`
	ComposeSHA256 string        `json:"compose_sha256,omitempty"`
	Entries       []BackupEntry `json:"entries"`
}

type CheckResult struct {
	Name       string `json:"name"`
	Service    string `json:"service"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type ServiceRecovery struct {
	Project       string        `json:"project"`
	Service       string        `json:"service"`
	Configuration string        `json:"configuration"`
	DataRestore   string        `json:"data_restore"`
	Startup       string        `json:"startup"`
	Health        string        `json:"health"`
	Reason        string        `json:"reason,omitempty"`
	Checks        []CheckResult `json:"checks,omitempty"`
}

type RecoveryReport struct {
	RunID          string            `json:"run_id"`
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     time.Time         `json:"finished_at"`
	Status         string            `json:"status"`
	ProjectName    string            `json:"recovery_project_name"`
	ComposeSHA256  string            `json:"compose_sha256"`
	BackupPath     string            `json:"backup_path"`
	BackupManifest string            `json:"backup_manifest_sha256"`
	Services       []ServiceRecovery `json:"services"`
	Findings       []Finding         `json:"findings,omitempty"`
	CleanupStatus  string            `json:"cleanup_status"`
	EvidencePath   string            `json:"evidence_path,omitempty"`
	ToolVersion    string            `json:"tool_version"`
}
