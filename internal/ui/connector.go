package ui

import (
	"context"
	"time"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/secrets"
)

// Settings es la configuración editable de la TUI (alias de application.Settings).
type Settings = application.Settings

// AppConnector define lo que la TUI necesita de la aplicación. Por defecto se
// usa *application.App; en tests puede inyectarse un doble.
type AppConnector interface {
	GetSettings() Settings
	SaveSettings(s Settings) error
	ConfigPath() string
	GetTUIStatus(ctx context.Context) (application.BackupStatus, []application.BackendStatus, error)
	Status(ctx context.Context) (*application.StatusReport, error)
	TailLogs(ctx context.Context, n int) ([]string, error)
	SaveR2Credentials(endpoint, bucket, accessKeyID, secretAccessKey string) error
	GetR2Credentials() (*secrets.Credentials, error)
	ProfileName() string
	ListProfiles() []application.ProfileInfo
	ActiveProfileDetail() application.ProfileDetail
	UseProfile(name string) error
	RemoteSyncTimeout() time.Duration
	CheckPlatforms(ctx context.Context) []application.PlatformCheck
	Backup(ctx context.Context, opts application.BackupOptions) error
	Sync(ctx context.Context, opts application.SyncOptions) error
	ExportConfiguration(outputPath, password string) error
	ImportConfiguration(inputPath, password string) error
}

// compile-time: *application.App satisface AppConnector.
var _ AppConnector = (*application.App)(nil)