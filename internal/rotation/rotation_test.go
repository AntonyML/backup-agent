package rotation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mustFiles(t *testing.T, dir string, names []string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlan_FewerThanKeep(t *testing.T) {
	files := []string{
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
	}
	del, err := Plan(files, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(del) != 0 {
		t.Errorf("con menos de N no debería borrar nada, borra %v", del)
	}
}

func TestPlan_ExactlyKeep(t *testing.T) {
	files := []string{
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
		"CONTABILIDAD_20260103_1200.bak",
	}
	del, err := Plan(files, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(del) != 0 {
		t.Errorf("con exactamente N no debería borrar nada, borra %v", del)
	}
}

func TestPlan_MoreThanKeep_DeletesOldest(t *testing.T) {
	// Entrada desordenada a propósito: Plan debe ordenar.
	files := []string{
		"CONTABILIDAD_20260105_1200.bak",
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260103_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
		"CONTABILIDAD_20260104_1200.bak",
	}
	del, err := Plan(files, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
	}
	if !reflect.DeepEqual(del, want) {
		t.Errorf("Plan = %v, quiero %v", del, want)
	}
}

func TestPlan_IgnoresTmpAndOtherFiles(t *testing.T) {
	files := []string{
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
		"CONTABILIDAD_20260103_1200.bak",
		"CONTABILIDAD_20260104_1200.bak.tmp", // huérfano: no cuenta
		"state.json",
		"agent.lock",
	}
	del, err := Plan(files, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(del) != 0 {
		t.Errorf("los .tmp y otros archivos no deben contar para rotación, borra %v", del)
	}
}

func TestPlan_InvalidKeep(t *testing.T) {
	if _, err := Plan([]string{"a.bak"}, 0); err == nil {
		t.Error("keep=0 debería devolver error")
	}
	if _, err := Plan([]string{"a.bak"}, -1); err == nil {
		t.Error("keep negativo debería devolver error")
	}
}

func TestRotate_DeletesOldestOnDisk_IgnoresTmp(t *testing.T) {
	dir := t.TempDir()
	mustFiles(t, dir, []string{
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
		"CONTABILIDAD_20260103_1200.bak",
		"CONTABILIDAD_20260104_1200.bak",
		"CONTABILIDAD_20260105_1200.bak.tmp", // debe sobrevivir
		"state.json",                         // debe sobrevivir
	})

	deleted, err := Rotate(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 {
		t.Fatalf("Rotate debería borrar 1, borró %v", deleted)
	}

	for _, gone := range []string{
		"CONTABILIDAD_20260101_1200.bak",
	} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s debería haber sido borrado", gone)
		}
	}
	for _, stays := range []string{
		"CONTABILIDAD_20260102_1200.bak",
		"CONTABILIDAD_20260103_1200.bak",
		"CONTABILIDAD_20260104_1200.bak",
		"CONTABILIDAD_20260105_1200.bak.tmp",
		"state.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, stays)); err != nil {
			t.Errorf("%s debería seguir existiendo: %v", stays, err)
		}
	}
}

func TestRotate_MissingDir_Errors(t *testing.T) {
	if _, err := Rotate(filepath.Join(t.TempDir(), "no-existe"), 3); err == nil {
		t.Error("Rotate en dir inexistente debería devolver error")
	}
}

func TestRotate_FiveBackupsKeepsThree(t *testing.T) {
	dir := t.TempDir()
	mustFiles(t, dir, []string{
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
		"CONTABILIDAD_20260103_1200.bak",
		"CONTABILIDAD_20260104_1200.bak",
		"CONTABILIDAD_20260105_1200.bak",
	})

	deleted, err := Rotate(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"CONTABILIDAD_20260101_1200.bak",
		"CONTABILIDAD_20260102_1200.bak",
	}
	if !reflect.DeepEqual(deleted, want) {
		t.Errorf("Rotate = %v, quiero %v", deleted, want)
	}
	remaining, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 3 {
		t.Errorf("deberían quedar 3, quedan %v", remaining)
	}
}
