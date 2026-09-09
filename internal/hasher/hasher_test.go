package hasher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBytes_KnownVectors(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Vectores conocidos de SHA-256 (verificables con: echo -n "abc" | sha256sum)
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{"hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
	}
	for _, c := range cases {
		if got := Bytes([]byte(c.input)); got != c.want {
			t.Errorf("Bytes(%q) = %s, quiero %s", c.input, got, c.want)
		}
	}
}

func TestReader_StreamsCorrectly(t *testing.T) {
	got, err := Reader(strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("Reader: %v", err)
	}
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("Reader = %s, quiero %s", got, want)
	}
}

func TestFile_RoundTripAndMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prueba.bak")
	content := []byte("contenido de backup de prueba\nsegunda linea\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := File(path)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if want := Bytes(content); got != want {
		t.Errorf("File = %s, quiero %s", got, want)
	}

	if _, err := File(filepath.Join(dir, "no-existe.bak")); err == nil {
		t.Error("File con archivo inexistente debería devolver error")
	}
}
