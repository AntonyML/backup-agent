package sqlbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// ErrInsufficientSpace se retorna cuando el espacio libre es inferior al estimado.
var ErrInsufficientSpace = errors.New("sqlbackup: espacio insuficiente en disco")


// DriverName es el nombre del driver registrado en database/sql.
const DriverName = "sqlserver"

// appName identifica nuestras conexiones en sys.dm_exec_sessions.
const appName = "femucaribe-backup-agent"

// dbNameRe restringe el nombre de base a identificadores simples para
// poder interpolarlo como [nombre] sin riesgo de inyección SQL.
// (La config ya lo valida; acá se re-valida por defensa en profundidad.)
var dbNameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// ValidateDatabaseName rechaza nombres que no sean identificadores simples.
func ValidateDatabaseName(name string) error {
	if !dbNameRe.MatchString(name) {
		return fmt.Errorf("sqlbackup: database %q inválida (solo letras, dígitos y _)", name)
	}
	return nil
}

// QuoteIdent envuelve un identificador en [corchetes], escapando `]`.
func QuoteIdent(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// QuoteStringLiteral devuelve un literal N'...' escapando comillas simples.
func QuoteStringLiteral(s string) string {
	return "N'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ConnectOptions parametriza la conexión hacia SQL Server.
type ConnectOptions struct {
	Server          string
	Database        string
	AuthMode        string // "windows" (default) o "sql"
	User            string
	Password        string
	LoginTimeoutSec int
}

// DSN arma el connection string ADO con Windows Integrated Auth o SQL Server Auth.
// Si AuthMode es "sql" (o hay User), usa user id/password con cifrado relajado para Docker.
// De lo contrario (default "windows"), usa trusted connection=yes (Single-Sign-On).
func DSN(opts ConnectOptions) string {
	loginTimeoutSec := opts.LoginTimeoutSec
	if loginTimeoutSec < 0 {
		loginTimeoutSec = 0
	}
	dbName := "master"
	if strings.TrimSpace(opts.Database) != "" {
		dbName = opts.Database
	}

	if strings.ToLower(strings.TrimSpace(opts.AuthMode)) == "sql" || strings.TrimSpace(opts.User) != "" {
		return fmt.Sprintf("server=%s;database=%s;user id=%s;password=%s;encrypt=disable;trustservercertificate=true;app name=%s;connection timeout=%d;dial timeout=%d",
			opts.Server, dbName, opts.User, opts.Password, appName, loginTimeoutSec, loginTimeoutSec)
	}

	return fmt.Sprintf("server=%s;database=%s;trusted connection=yes;app name=%s;connection timeout=%d;dial timeout=%d",
		opts.Server, dbName, appName, loginTimeoutSec, loginTimeoutSec)
}

// Open abre el pool y verifica conectividad con un Ping con timeout.
// Falla rápido si SQL Server está detenido o la instancia es inaccesible.
func Open(opts ConnectOptions) (*sql.DB, error) {
	if strings.TrimSpace(opts.Server) == "" {
		return nil, fmt.Errorf("sqlbackup: server vacío")
	}
	db, err := sql.Open(DriverName, DSN(opts))
	if err != nil {
		return nil, fmt.Errorf("sqlbackup: abrir conexión a %s: %w", opts.Server, err)
	}
	db.SetMaxOpenConns(1)
	timeout := time.Duration(opts.LoginTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlbackup: SQL Server %q inaccesible (¿detenido o instancia incorrecta?): %w", opts.Server, err)
	}
	return db, nil
}

// BuildBackupSQL arma el BACKUP DATABASE hacia un temporal.
// WITH INIT (sobreescribe el temporal), COMPRESSION (2008R2+),
// CHECKSUM (detecta corrupción de páginas) y STATS=5 (progreso).
func BuildBackupSQL(database, destPath string) (string, error) {
	if err := ValidateDatabaseName(database); err != nil {
		return "", err
	}
	if strings.TrimSpace(destPath) == "" {
		return "", fmt.Errorf("sqlbackup: ruta destino vacía")
	}
	return fmt.Sprintf("BACKUP DATABASE %s TO DISK = %s WITH INIT, COMPRESSION, CHECKSUM, STATS = 5",
		QuoteIdent(database), QuoteStringLiteral(destPath)), nil
}

// BackupDatabase ejecuta BACKUP DATABASE al archivo temporal.
// El ctx del llamador gobierna el timeout (puede tardar minutos/GBs).
// La ruta debe ser local AL SERVIDOR SQL: BACKUP escribe desde el
// servicio SQL, no desde este proceso.
func BackupDatabase(ctx context.Context, db *sql.DB, database, destPath string) error {
	q, err := BuildBackupSQL(database, destPath)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, q); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("sqlbackup: BACKUP DATABASE %s excedió el timeout: %w", database, err)
		}
		return fmt.Errorf("sqlbackup: BACKUP DATABASE %s falló: %w", database, err)
	}
	return nil
}

// BuildVerifySQL arma el RESTORE VERIFYONLY sobre un backup existente.
func BuildVerifySQL(backupPath string) (string, error) {
	if strings.TrimSpace(backupPath) == "" {
		return "", fmt.Errorf("sqlbackup: ruta vacía")
	}
	return fmt.Sprintf("RESTORE VERIFYONLY FROM DISK = %s", QuoteStringLiteral(backupPath)), nil
}

// VerifyBackup corre RESTORE VERIFYONLY: valida que el .bak sea legible
// y completo sin restaurar nada.
func VerifyBackup(ctx context.Context, db *sql.DB, backupPath string) error {
	q, err := BuildVerifySQL(backupPath)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("sqlbackup: RESTORE VERIFYONLY de %s falló (backup inválido): %w", backupPath, err)
	}
	return nil
}

// DatabaseSizeBytes estima el tamaño de la base sumando sus archivos
// (sys.database_files.size está en páginas de 8 KB).
func DatabaseSizeBytes(ctx context.Context, db *sql.DB, database string) (int64, error) {
	if err := ValidateDatabaseName(database); err != nil {
		return 0, err
	}
	q := fmt.Sprintf("SELECT COALESCE(SUM(CAST(size AS BIGINT)) * 8192, 0) FROM %s.sys.database_files",
		QuoteIdent(database))
	var size int64
	if err := db.QueryRowContext(ctx, q).Scan(&size); err != nil {
		return 0, fmt.Errorf("sqlbackup: no se pudo estimar el tamaño de %s (¿existe la base?): %w", database, err)
	}
	return size, nil
}

// EnsureFreeSpace falla rápido si el volumen de dir no tiene al menos
// `needed` bytes libres. El llamador debe crear dir antes (el chequeo
// de espacio del SO requiere una ruta existente en el volumen).
func EnsureFreeSpace(dir string, needed int64) error {
	free, err := FreeBytes(dir)
	if err != nil {
		return fmt.Errorf("sqlbackup: no se pudo medir espacio libre en %s: %w", dir, err)
	}
	if free < needed {
		return fmt.Errorf("%w en %s: libres %.2f GB, se necesitan ~%.2f GB para respaldar",
			ErrInsufficientSpace, dir, gb(free), gb(needed))
	}

	return nil
}

func gb(b int64) float64 { return float64(b) / (1024 * 1024 * 1024) }
