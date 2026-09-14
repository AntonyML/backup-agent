package ui

import (
	"strings"
	"testing"
)

func TestMultiselect_Basics(t *testing.T) {
	ms := weekdaysMultiselect([]string{"mon", "wed", "fri"})

	// 1. checked inicial
	chk := ms.checked()
	if len(chk) != 3 || chk[0] != "mon" || chk[1] != "wed" || chk[2] != "fri" {
		t.Fatalf("checked inicial inesperado: %v", chk)
	}

	// 2. toggle en cursor actual (0 = mon)
	ms.toggle()
	chk = ms.checked()
	if len(chk) != 2 || chk[0] != "wed" || chk[1] != "fri" {
		t.Fatalf("tras desmarcar mon se esperaba [wed fri], dio: %v", chk)
	}

	// 3. move cíclico hacia atrás (-1 debe ir al último elemento: sun)
	ms.move(-1)
	if ms.Cursor != len(ms.Items)-1 {
		t.Fatalf("move(-1) esperaba cursor %d, dio %d", len(ms.Items)-1, ms.Cursor)
	}

	// 4. marcar sun
	ms.toggle()
	chk = ms.checked()
	if len(chk) != 3 || chk[2] != "sun" {
		t.Fatalf("tras marcar sun se esperaba último sun, dio: %v", chk)
	}

	// 5. move cíclico hacia adelante (+1 desde el final debe ir a 0)
	ms.move(1)
	if ms.Cursor != 0 {
		t.Fatalf("move(1) desde final esperaba cursor 0, dio %d", ms.Cursor)
	}

	// 6. setChecked explícito
	ms.setChecked([]string{"tue", "thu"})
	chk = ms.checked()
	if len(chk) != 2 || chk[0] != "tue" || chk[1] != "thu" {
		t.Fatalf("setChecked esperaba [tue thu], dio: %v", chk)
	}

	// 7. viewMultiselect renderiza correctamente
	view := viewMultiselect(ms, DefaultStyles())
	if !strings.Contains(view, "[x]") || !strings.Contains(view, "[ ]") {
		t.Errorf("viewMultiselect no renderiza cajas de checkbox: %s", view)
	}
	if !strings.Contains(view, "Martes (tue)") {
		t.Errorf("viewMultiselect no incluye etiquetas: %s", view)
	}
}

func TestPlatformsMultiselect(t *testing.T) {
	ms := platformsMultiselect([]string{"cloudflare"})
	chk := ms.checked()
	if len(chk) != 1 || chk[0] != "cloudflare" {
		t.Fatalf("platformsMultiselect esperaba [cloudflare], dio %v", chk)
	}

	ms.move(1) // posicionarse en remote_server
	ms.toggle()
	chk = ms.checked()
	if len(chk) != 2 {
		t.Fatalf("esperaba 2 plataformas marcadas, dio %v", chk)
	}
}

func TestConfirmModel(t *testing.T) {
	cm := newConfirmModel("Confirmación requerida", "¿Desea continuar con la operación?")

	// Estado inicial: ok == false
	if cm.ok {
		t.Errorf("confirmModel debería iniciar con ok = false")
	}

	// Alternar foco
	cm.move()
	if !cm.ok {
		t.Errorf("tras move() debería estar en true (Sí)")
	}
	cm.move()
	if cm.ok {
		t.Errorf("tras segundo move() debería volver a false (No)")
	}

	// Render view
	styles := DefaultStyles()
	view := cm.view(styles)
	if !strings.Contains(view, "Confirmación requerida") {
		t.Errorf("título no renderizado en confirmModel: %s", view)
	}
	if !strings.Contains(view, "[ Sí ]") || !strings.Contains(view, "[ No ]") {
		t.Errorf("opciones Sí/No no renderizadas en confirmModel: %s", view)
	}
}
