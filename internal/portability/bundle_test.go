package portability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/secrets"
)

func TestExportAndImportRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	datPath := filepath.Join(tempDir, "config.dat")
	outPath := filepath.Join(tempDir, "export.bacfg")

	// 1. Crear config inicial
	initialCfg := config.Default()
	initialCfg.Database = "TEST_ROUNDTRIP_DB"
	initialCfg.Server = "SRV-TEST"
	if err := config.Save(cfgPath, initialCfg); err != nil {
		t.Fatalf("guardar config inicial: %v", err)
	}

	// 2. Crear credenciales iniciales
	initialCreds := secrets.Credentials{
		Endpoint:        "https://test.r2.cloudflarestorage.com",
		Bucket:          "my-test-bucket",
		AccessKeyID:     "AKIA-TEST",
		SecretAccessKey: "SECRET-TEST-KEY",
	}
	if err := secrets.Save(datPath, initialCreds); err != nil {
		t.Fatalf("guardar credenciales iniciales: %v", err)
	}

	pass := "MiPasswordSuperSeguro123!"

	// 3. Exportar
	if err := Export(cfgPath, datPath, outPath, pass); err != nil {
		t.Fatalf("Export fallo: %v", err)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("archivo exportado no existe: %v", err)
	}

	// 4. Importar en nuevas rutas
	newCfgPath := filepath.Join(tempDir, "imported_config.json")
	newDatPath := filepath.Join(tempDir, "imported_config.dat")

	importedCfg, err := Import(outPath, pass, newCfgPath, newDatPath)
	if err != nil {
		t.Fatalf("Import fallo: %v", err)
	}

	if importedCfg.Database != "TEST_ROUNDTRIP_DB" {
		t.Errorf("esperaba DB TEST_ROUNDTRIP_DB, obtuve %s", importedCfg.Database)
	}
	if importedCfg.Server != "SRV-TEST" {
		t.Errorf("esperaba Server SRV-TEST, obtuve %s", importedCfg.Server)
	}

	// 5. Verificar que las credenciales se descifran y re-cifran correctamente
	importedCreds, err := secrets.Load(newDatPath)
	if err != nil {
		t.Fatalf("cargar credenciales importadas: %v", err)
	}
	if importedCreds.Endpoint != initialCreds.Endpoint {
		t.Errorf("Endpoint no coincide: %s vs %s", importedCreds.Endpoint, initialCreds.Endpoint)
	}
	if importedCreds.Bucket != initialCreds.Bucket {
		t.Errorf("Bucket no coincide: %s vs %s", importedCreds.Bucket, initialCreds.Bucket)
	}
	if importedCreds.AccessKeyID != initialCreds.AccessKeyID {
		t.Errorf("AccessKeyID no coincide: %s vs %s", importedCreds.AccessKeyID, initialCreds.AccessKeyID)
	}
	if importedCreds.SecretAccessKey != initialCreds.SecretAccessKey {
		t.Errorf("SecretAccessKey no coincide: %s vs %s", importedCreds.SecretAccessKey, initialCreds.SecretAccessKey)
	}
}

func TestImportWrongPassword(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	outPath := filepath.Join(tempDir, "export.bacfg")

	initialCfg := config.Default()
	if err := config.Save(cfgPath, initialCfg); err != nil {
		t.Fatalf("guardar config: %v", err)
	}

	pass := "CorrectPassword123"
	if err := Export(cfgPath, "", outPath, pass); err != nil {
		t.Fatalf("Export fallo: %v", err)
	}

	newCfgPath := filepath.Join(tempDir, "imported_config.json")
	_, err := Import(outPath, "WrongPassword999", newCfgPath, "")
	if err == nil {
		t.Fatal("esperaba error al importar con clave incorrecta, obtuve nil")
	}
	if err != ErrInvalidPassword {
		t.Errorf("esperaba ErrInvalidPassword, obtuve: %v", err)
	}
}

func TestExportShortPassword(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	outPath := filepath.Join(tempDir, "export.bacfg")

	initialCfg := config.Default()
	_ = config.Save(cfgPath, initialCfg)

	if err := Export(cfgPath, "", outPath, "123"); err != ErrShortPassword {
		t.Errorf("esperaba ErrShortPassword, obtuve %v", err)
	}
	if err := Export(cfgPath, "", outPath, ""); err != ErrEmptyPassword {
		t.Errorf("esperaba ErrEmptyPassword, obtuve %v", err)
	}
}

func TestImportTamperedFile(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	outPath := filepath.Join(tempDir, "export.bacfg")

	initialCfg := config.Default()
	_ = config.Save(cfgPath, initialCfg)

	pass := "MySecretPassword123"
	if err := Export(cfgPath, "", outPath, pass); err != nil {
		t.Fatalf("Export fallo: %v", err)
	}

	// Alterar el archivo
	data, _ := os.ReadFile(outPath)
	var env Envelope
	_ = json.Unmarshal(data, &env)
	env.Ciphertext = "AAAA" + env.Ciphertext[4:]
	alteredData, _ := json.Marshal(env)
	_ = os.WriteFile(outPath, alteredData, 0o600)

	newCfgPath := filepath.Join(tempDir, "imported_config.json")
	_, err := Import(outPath, pass, newCfgPath, "")
	if err == nil {
		t.Fatal("esperaba fallo por tamper de archivo")
	}
}
