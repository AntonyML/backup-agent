package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault_Valid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default() debería ser válida: %v", err)
	}
	if Default().Retain != 3 {
		t.Errorf("Retain default debería ser 3, es %d", Default().Retain)
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("Load inexistente no debería dar error: %v", err)
	}
	if cfg != Default() {
		t.Errorf("Load inexistente debería devolver defaults, dio %+v", cfg)
	}
}

func TestLoad_PartialOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"retain": 5, "database": "PRUEBAS"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Retain != 5 || cfg.Database != "PRUEBAS" {
		t.Errorf("override parcial no aplicado: %+v", cfg)
	}
	if cfg.Server != Default().Server || cfg.BackupDir != Default().BackupDir {
		t.Errorf("campos no presentes deberían quedar en default: %+v", cfg)
	}
}

func TestLoad_Corrupt_Errors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{json roto"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load corrupto debería devolver error")
	}
}

func TestLoad_InvalidValues_Errors(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"retain cero", `{"retain": 0}`},
		{"retain negativo", `{"retain": -2}`},
		{"database con punto y coma (inyección)", `{"database": "x; DROP TABLE y"}`},
		{"database con comilla", `{"database": "a'b"}`},
		{"database con corchete", `{"database": "a]b"}`},
		{"database vacía", `{"database": ""}`},
		{"server vacío", `{"server": ""}`},
		{"backup_dir vacío", `{"backup_dir": ""}`},
		{"timeout negativo", `{"login_timeout_sec": -1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(c.json), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Errorf("Load(%s) debería devolver error", c.json)
			}
		})
	}
}
