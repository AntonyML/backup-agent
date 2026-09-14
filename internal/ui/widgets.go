package ui

import (
	"fmt"
	"strings"
)

// multiselect es un primitive de checkboxes para la TUI: cada opción tiene
// etiqueta y puede estar marcada. Se navega con ↑/↓ y se alterna con espacio.
// El orden de Items determina el orden de display.
type multiselect struct {
	Items  []multiselectItem
	Cursor int
}

type multiselectItem struct {
	Value   string
	Label   string
	Checked bool
}

// checked devuelve los valores marcados, en orden de display.
func (m multiselect) checked() []string {
	out := []string{}
	for _, it := range m.Items {
		if it.Checked {
			out = append(out, it.Value)
		}
	}
	return out
}

// setChecked marca exactamente los valores indicados (el resto se desmarca).
func (m *multiselect) setChecked(values []string) {
	want := map[string]bool{}
	for _, v := range values {
		want[v] = true
	}
	for i := range m.Items {
		m.Items[i].Checked = want[m.Items[i].Value]
	}
}

// move mueve el cursor cíclicamente.
func (m *multiselect) move(delta int) {
	if len(m.Items) == 0 {
		return
	}
	m.Cursor = (m.Cursor + delta + len(m.Items)) % len(m.Items)
}

// toggle alterna el ítem bajo el cursor.
func (m *multiselect) toggle() {
	if len(m.Items) == 0 || m.Cursor < 0 || m.Cursor >= len(m.Items) {
		return
	}
	m.Items[m.Cursor].Checked = !m.Items[m.Cursor].Checked
}

// weekdaysMultiselect arma un selector de días mon..sun con los días dados marcados.
func weekdaysMultiselect(checked []string) multiselect {
	want := map[string]bool{}
	for _, d := range checked {
		want[d] = true
	}
	labels := map[string]string{
		"mon": "Lunes (mon)", "tue": "Martes (tue)", "wed": "Miércoles (wed)",
		"thu": "Jueves (thu)", "fri": "Viernes (fri)", "sat": "Sábado (sat)", "sun": "Domingo (sun)",
	}
	order := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	items := make([]multiselectItem, 0, len(order))
	for _, v := range order {
		items = append(items, multiselectItem{Value: v, Label: labels[v], Checked: want[v]})
	}
	return multiselect{Items: items}
}

// platformsMultiselect arma un selector de plataformas remotas.
func platformsMultiselect(checked []string) multiselect {
	want := map[string]bool{}
	for _, p := range checked {
		want[p] = true
	}
	return multiselect{Items: []multiselectItem{
		{Value: "cloudflare", Label: "Cloudflare R2 (nube)", Checked: want["cloudflare"]},
		{Value: "remote_server", Label: "Servidor externo (UNC)", Checked: want["remote_server"]},
	}}
}

// viewMultiselect renderiza el selector con el estilo dado.
func viewMultiselect(m multiselect, styles Styles) string {
	var b strings.Builder
	for i, it := range m.Items {
		box := "[ ]"
		if it.Checked {
			box = "[x]"
		}
		line := fmt.Sprintf("%s %s", box, it.Label)
		if i == m.Cursor {
			b.WriteString(styles.InputPrompt.Render("▶ " + line))
		} else {
			b.WriteString("  " + styles.Desc.Render(line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// confirmModel es un diálogo modal de confirmación (sí/no) para acciones
// destructivas (eliminar perfil, instalar tarea). Navegar con ←/→ o tab,
// confirmar con enter, cancelar con esc.
type confirmModel struct {
	title string
	text  string
	ok    bool // foco actual: true = "Sí", false = "No"
}

func newConfirmModel(title, text string) confirmModel {
	return confirmModel{title: title, text: text}
}

func (m *confirmModel) move() { m.ok = !m.ok }

func (m confirmModel) view(styles Styles) string {
	var b strings.Builder
	b.WriteString(styles.AppTitle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(styles.Desc.Render(m.text))
	b.WriteString("\n\n")
	yes, no := "[ Sí ]", "[ No ]"
	if m.ok {
		yes = styles.InputPrompt.Render("▶ [ Sí ]")
		no = styles.Desc.Render("  [ No ]")
	} else {
		yes = styles.Desc.Render("  [ Sí ]")
		no = styles.InputPrompt.Render("▶ [ No ]")
	}
	b.WriteString(yes + "    " + no)
	b.WriteString("\n\n")
	b.WriteString(styles.HelpBar.Render(
		styles.Key.Render("[←/→, Tab]") + " " + styles.Desc.Render("Cambiar") + "  " +
			styles.Key.Render("[Enter]") + " " + styles.Desc.Render("Confirmar") + "  " +
			styles.Key.Render("[Esc]") + " " + styles.Desc.Render("Cancelar")))
	return styles.Box.Render(b.String())
}