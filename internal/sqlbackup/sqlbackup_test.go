package sqlbackup

import (
	"os"
	"strings"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"
)

func TestDSN_WindowsAuth(t *testing.T) {
	dsn := DSN(ConnectOptions{Server: `Caproba01\vbadilla`, LoginTimeoutSec: 15})
	// Debe parsearlo el propio driver (sin conectarse).
	cfg, err := msdsn.Parse(dsn)
	if err != nil {
		t.Fatalf("el driver debería parsear nuestro DSN %q: %v", dsn, err)
	}
	if !strings.Contains(cfg.Host, "Caproba01") {
		t.Errorf("host parseado = %q, esperaba Caproba01\\vbadilla", cfg.Host)
	}
	for _, want := range []string{"trusted connection=yes", "database=master"} {
		if !strings.Contains(strings.ToLower(dsn), want) {
			t.Errorf("DSN debería contener %q: %s", want, dsn)
		}
	}
	if strings.Contains(strings.ToLower(dsn), "password") || strings.Contains(strings.ToLower(dsn), "user id") {
		t.Errorf("DSN no debe llevar credenciales (Windows Integrated Auth): %s", dsn)
	}
}

func TestDSN_SQLAuth(t *testing.T) {
	dsn := DSN(ConnectOptions{
		Server:          "localhost,14333",
		Database:        "SIDC",
		AuthMode:        "sql",
		User:            "sa",
		Password:        "SecretPass123!",
		LoginTimeoutSec: 10,
	})
	cfg, err := msdsn.Parse(dsn)
	if err != nil {
		t.Fatalf("el driver debería parsear DSN de SQL auth %q: %v", dsn, err)
	}
	if !strings.Contains(cfg.Host, "localhost") {
		t.Errorf("host parseado = %q, esperaba localhost", cfg.Host)
	}
	for _, want := range []string{"user id=sa", "password=SecretPass123!", "database=SIDC", "trustservercertificate=true"} {
		if !strings.Contains(strings.ToLower(dsn), strings.ToLower(want)) {
			t.Errorf("DSN debería contener %q: %s", want, dsn)
		}
	}
	if strings.Contains(strings.ToLower(dsn), "trusted connection=yes") {
		t.Errorf("DSN en modo SQL no debe contener trusted connection: %s", dsn)
	}
}

func TestBuildBackupSQL(t *testing.T) {
	q, err := BuildBackupSQL("CONTABILIDAD", `C:\Backups\CONTABILIDAD_20260101_1200.bak.tmp`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"BACKUP DATABASE [CONTABILIDAD]",
		`N'C:\Backups\CONTABILIDAD_20260101_1200.bak.tmp'`,
		"WITH INIT", "COMPRESSION", "CHECKSUM",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("BACKUP SQL debería contener %q:\n%s", want, q)
		}
	}
	if _, err := BuildBackupSQL("a; DROP TABLE b", `C:\x.bak`); err == nil {
		t.Error("database con inyección debería ser rechazada")
	}
	if _, err := BuildBackupSQL("CONTABILIDAD", ""); err == nil {
		t.Error("destino vacío debería ser rechazado")
	}
}

func TestQuoteEscaping(t *testing.T) {
	if got := QuoteStringLiteral(`C:\a'b.bak`); got != `N'C:\a''b.bak'` {
		t.Errorf("QuoteStringLiteral = %s", got)
	}
	if got := QuoteIdent("a]b"); got != "[a]]b]" {
		t.Errorf("QuoteIdent = %s", got)
	}
	if err := ValidateDatabaseName("CONTABILIDAD"); err != nil {
		t.Errorf("CONTABILIDAD válida rechazada: %v", err)
	}
	if err := ValidateDatabaseName("a'b"); err == nil {
		t.Error("nombre con comilla debería ser rechazado")
	}
}

func TestBuildVerifySQL(t *testing.T) {
	q, err := BuildVerifySQL(`C:\Backups\x.bak.tmp`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "RESTORE VERIFYONLY") || !strings.Contains(q, `N'C:\Backups\x.bak.tmp'`) {
		t.Errorf("VERIFY SQL inesperado: %s", q)
	}
}

func TestEnsureFreeSpace_RealVolume(t *testing.T) {
	dir := t.TempDir()
	// 1 byte siempre debería caber; si falla, el chequeo está roto.
	if err := EnsureFreeSpace(dir, 1); err != nil {
		t.Fatalf("EnsureFreeSpace(dir, 1): %v", err)
	}
	// 1 exabyte nunca debería caber.
	if err := EnsureFreeSpace(dir, 1<<60); err == nil {
		t.Error("EnsureFreeSpace con tamaño imposible debería fallar")
	}
}

func TestFreeBytes_MissingPath_Errors(t *testing.T) {
	missing := t.TempDir() + string(os.PathSeparator) + "no-existe"
	if _, err := FreeBytes(missing); err == nil {
		t.Error("FreeBytes en ruta inexistente debería dar error")
	}
}
