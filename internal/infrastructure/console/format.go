package console

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Ayudas de formato compartidas por todas las vistas.

const shortIDLength = 7

// ShortID es el final de un id: la parte que distingue a un UUID v7 de los que se crearon cerca.
func ShortID(id string) string {
	if len(id) <= shortIDLength {
		return id
	}
	return id[len(id)-shortIDLength:]
}

// Ago dice hace cuánto fue t, en palabras que una persona usaría.
func Ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case t.IsZero():
		return "—"
	case d < 0:
		return "ahora"
	case d < time.Minute:
		return "hace unos segundos"
	case d < time.Hour:
		return fmt.Sprintf("hace %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("hace %d h", int(d.Hours()))
	case d < 48*time.Hour:
		return "ayer"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("hace %d d", int(d.Hours()/24))
	}
	return t.Local().Format("2 Jan 2006")
}

// Duration muestra una duración con la precisión que importa: milisegundos si es breve, segundos o minutos después.
func Duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Round(time.Second).Seconds()))
	case d < time.Hour:
		d = d.Round(time.Second)
		if seconds := int(d.Seconds()) % 60; seconds != 0 {
			return fmt.Sprintf("%d min %d s", int(d.Minutes()), seconds)
		}
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	d = d.Round(time.Minute)
	if minutes := int(d.Minutes()) % 60; minutes != 0 {
		return fmt.Sprintf("%d h %d min", int(d.Hours()), minutes)
	}
	return fmt.Sprintf("%d h", int(d.Hours()))
}

// ansi son las secuencias de color de la terminal: ocupan bytes pero no ocupan lugar al pintarse.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleWidth es el ancho con que una cadena se ve en la terminal: sin colores y contando caracteres, no bytes.
func visibleWidth(s string) int {
	return utf8.RuneCountInString(ansi.ReplaceAllString(s, ""))
}

// columnGap es el espacio entre columnas.
const columnGap = 2

// Table escribe filas alineadas por columnas, con la primera fila como encabezado. Mide el ancho visible de cada
// celda: ni los símbolos (✔ ✘) ni los colores descuadran la tabla. La última columna no se rellena.
func Table(w io.Writer, rows [][]string) {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], visibleWidth(cell))
		}
	}
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			line.WriteString(cell)
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-visibleWidth(cell)+columnGap))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(line.String(), " ")) // sin espacios al final: ensucian al copiar
	}
}

// Indent sangra cada línea de un texto.
func Indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// Plural elige entre singular y plural según n.
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
