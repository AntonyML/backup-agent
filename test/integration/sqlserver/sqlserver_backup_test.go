//go:build sqlserver

package sqlserver_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/test/fixtures"
	"femucaribe-backup-agent/test/helpers"
	"femucaribe-backup-agent/test/testdb"
	"femucaribe-backup-agent/test/testenv"
	_ "github.com/microsoft/go-mssqldb"
)

func getSQLServerConfig() testdb.SQLServerConfig {
	cfg := testenv.LoadConfig()
	return testdb.SQLServerConfig{
		Host:       cfg.SQLServerHost,
		Port:       cfg.SQLServerPort,
		Database:   cfg.SQLServerDatabase,
		User:       cfg.SQLServerUser,
		Password:   cfg.SQLServerPassword,
		BackupRoot: cfg.BackupRoot,
	}
}

func TestSQLServer_FullBackupVerifyRestoreEquivalence(t *testing.T) {
	cfg := getSQLServerConfig()
	restoreDBName := cfg.Database + "_RESTORE"

	// 1. Validaciones estrictas de seguridad anti-producción
	if err := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlserver",
		DatabaseName:   cfg.Database,
		ServerInstance: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		BackupRoot:     cfg.BackupRoot,
		TestMode:       true,
	}); err != nil {
		t.Fatalf("seguridad violada en base origen: %v", err)
	}

	if err := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlserver",
		DatabaseName:   restoreDBName,
		ServerInstance: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		BackupRoot:     cfg.BackupRoot,
		TestMode:       true,
	}); err != nil {
		t.Fatalf("seguridad violada en base destino: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 2. Conectar a SQL Server aislado
	origDB, err := testdb.NewSQLServer(ctx, cfg)
	if err != nil {
		t.Skipf("SQL Server aislado no disponible en %s:%d (%v); saltando test de integración", cfg.Host, cfg.Port, err)
	}
	defer origDB.Close()

	// 3. Reset y Seed determinístico en CONTABILIDAD_TEST
	if err := origDB.Reset(ctx); err != nil {
		t.Fatalf("reset de base origen falló: %v", err)
	}
	if err := origDB.Seed(ctx); err != nil {
		t.Fatalf("seed de base origen falló: %v", err)
	}
	if err := fixtures.AssertSeedIntegrity(ctx, origDB.DB()); err != nil {
		t.Fatalf("integridad del seed en base origen falló: %v", err)
	}

	// 4. Determinar ruta de backup para el servidor SQL (compatible con Docker Linux o Windows)
	// En Docker Linux el servicio escribe en /var/opt/mssql/data/
	var backupTarget string
	if os.Getenv("TEST_SQLSERVER_CONTAINER_PATH") != "" {
		backupTarget = os.Getenv("TEST_SQLSERVER_CONTAINER_PATH")
	} else {
		// Detectamos si el servidor corre en Linux o Windows
		var isWindows int
		_ = origDB.DB().QueryRowContext(ctx, "SELECT CASE WHEN @@VERSION LIKE '%Windows%' THEN 1 ELSE 0 END").Scan(&isWindows)
		if isWindows == 1 {
			backupTarget = filepath.Join(cfg.BackupRoot, fmt.Sprintf("%s_integ.bak", cfg.Database))
		} else {
			backupTarget = fmt.Sprintf("/var/opt/mssql/data/%s_integ.bak", strings.ToLower(cfg.Database))
		}
	}

	// 5. Ejecutar BACKUP DATABASE
	if err := sqlbackup.BackupDatabase(ctx, origDB.DB(), cfg.Database, backupTarget); err != nil {
		t.Fatalf("BACKUP DATABASE falló: %v", err)
	}

	// 6. Ejecutar RESTORE VERIFYONLY
	if err := sqlbackup.VerifyBackup(ctx, origDB.DB(), backupTarget); err != nil {
		t.Fatalf("RESTORE VERIFYONLY falló: %v", err)
	}

	// 7. Averiguar nombres lógicos de los archivos del backup con RESTORE FILELISTONLY
	filelistQuery := fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = %s", sqlbackup.QuoteStringLiteral(backupTarget))
	rows, err := origDB.DB().QueryContext(ctx, filelistQuery)
	if err != nil {
		t.Fatalf("RESTORE FILELISTONLY falló: %v", err)
	}
	defer rows.Close()

	type fileInfo struct {
		logicalName string
		fileType    string
	}
	var files []fileInfo

	for rows.Next() {
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("error leyendo columnas de FILELISTONLY: %v", err)
		}
		vals := make([]interface{}, len(cols))
		valPtrs := make([]interface{}, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		if err := rows.Scan(valPtrs...); err != nil {
			t.Fatalf("error escaneando FILELISTONLY: %v", err)
		}

		var logical string
		var fType string
		for i, col := range cols {
			if strings.EqualFold(col, "LogicalName") {
				if s, ok := vals[i].(string); ok {
					logical = s
				}
			}
			if strings.EqualFold(col, "Type") {
				if s, ok := vals[i].(string); ok {
					fType = s
				}
			}
		}
		if logical != "" {
			files = append(files, fileInfo{logicalName: logical, fileType: fType})
		}
	}
	rows.Close()

	// 8. Restaurar backup en una base temporal aislada CONTABILIDAD_TEST_RESTORE
	var moveClauses []string
	for _, fi := range files {
		ext := "mdf"
		if fi.fileType == "L" {
			ext = "ldf"
		}
		destPhys := fmt.Sprintf("/var/opt/mssql/data/%s_%s.%s", restoreDBName, fi.logicalName, ext)
		moveClauses = append(moveClauses, fmt.Sprintf("MOVE %s TO %s",
			sqlbackup.QuoteStringLiteral(fi.logicalName),
			sqlbackup.QuoteStringLiteral(destPhys)))
	}

	restoreSQL := fmt.Sprintf("RESTORE DATABASE %s FROM DISK = %s WITH REPLACE",
		sqlbackup.QuoteIdent(restoreDBName),
		sqlbackup.QuoteStringLiteral(backupTarget))
	if len(moveClauses) > 0 {
		restoreSQL += ", " + strings.Join(moveClauses, ", ")
	}

	if _, err := origDB.DB().ExecContext(ctx, restoreSQL); err != nil {
		t.Fatalf("RESTORE DATABASE en base temporal falló: %v", err)
	}

	// Limpieza garantizada al finalizar el test
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		dropSQL := fmt.Sprintf("IF DB_ID('%s') IS NOT NULL BEGIN ALTER DATABASE %s SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE %s; END",
			restoreDBName, sqlbackup.QuoteIdent(restoreDBName), sqlbackup.QuoteIdent(restoreDBName))
		_, _ = origDB.DB().ExecContext(cleanupCtx, dropSQL)
	}()

	// 9. Conectar a la base restaurada y verificar integridad y equivalencia
	restoreCfg := cfg
	restoreCfg.Database = restoreDBName
	restoredDB, err := testdb.NewSQLServer(ctx, restoreCfg)
	if err != nil {
		t.Fatalf("conectar a base restaurada falló: %v", err)
	}
	defer restoredDB.Close()

	if err := fixtures.AssertSeedIntegrity(ctx, restoredDB.DB()); err != nil {
		t.Fatalf("la base restaurada falló AssertSeedIntegrity: %v", err)
	}

	// 10. Comparación de snapshot determinístico y equivalente
	helpers.AssertDatabaseEquivalentWithContext(ctx, t, origDB.DB(), restoredDB.DB())
}
