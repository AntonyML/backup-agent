package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
)

func TestExportModal_NavigationAndValidation(t *testing.T) {
	styles := DefaultStyles()
	mock := &mockAppConnector{settings: application.Settings{}}
	modal := newExportModalModel(mock, styles)

	// Inicialmente enfocado en 0 (ruta)
	if modal.focused != 0 {
		t.Errorf("esperaba focused=0, obtuve %d", modal.focused)
	}

	// Presionar tab para pasar a contraseña
	modal, _ = modal.update(tea.KeyPressMsg{Code: tea.KeyTab})
	if modal.focused != 1 {
		t.Errorf("esperaba focused=1 tras Tab, obtuve %d", modal.focused)
	}

	// Presionar enter en campo contraseña (focused=1) pasa a confirmación (focused=2)
	modal, _ = modal.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if modal.focused != 2 {
		t.Errorf("esperaba focused=2 tras Enter en contraseña, obtuve %d", modal.focused)
	}

	// Presionar enter en confirmación (focused=2) sin contraseña -> submit falla por contraseña vacía
	modal, _ = modal.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if modal.err == nil {
		t.Fatal("esperaba error por contraseña vacía")
	}

	// Asignar contraseñas distintas -> error
	modal.inputs[1].SetValue("password123")
	modal.inputs[2].SetValue("password456")
	modal.focused = 3 // Botón Exportar
	modal, _ = modal.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if modal.err == nil {
		t.Fatal("esperaba error por contraseñas que no coinciden")
	}

	// Asignar contraseñas iguales -> submit exitoso
	modal.inputs[2].SetValue("password123")
	modal, cmd := modal.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if modal.err != nil {
		t.Fatalf("error inesperado: %v", modal.err)
	}
	if cmd == nil {
		t.Fatal("esperaba cmd de exportación")
	}

	// Simular exportSuccessMsg
	modal, _ = modal.update(exportSuccessMsg{Path: "test.bacfg"})
	if modal.success == "" {
		t.Fatal("esperaba mensaje de éxito en modal")
	}
}

func TestImportModal_NavigationAndValidation(t *testing.T) {
	styles := DefaultStyles()
	mock := &mockAppConnector{settings: application.Settings{}}
	modal := newImportModalModel(mock, styles)

	// Presionar enter sin contraseña -> error
	modal.inputs[0].SetValue("test.bacfg")
	modal.inputs[1].SetValue("")
	modal.focused = 2 // Botón Importar
	modal, _ = modal.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if modal.err == nil {
		t.Fatal("esperaba error por contraseña vacía")
	}

	// Con contraseña -> submit exitoso
	modal.inputs[1].SetValue("clave123")
	modal, cmd := modal.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if modal.err != nil {
		t.Fatalf("error inesperado: %v", modal.err)
	}
	if cmd == nil {
		t.Fatal("esperaba cmd de importación")
	}

	// Simular importSuccessMsg
	modal, _ = modal.update(importSuccessMsg{Path: "test.bacfg"})
	if modal.success == "" {
		t.Fatal("esperaba mensaje de éxito en modal")
	}
}
