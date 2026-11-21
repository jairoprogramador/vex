package application

import (
	"fmt"
	"strings"
)

// ExternalText deja listo para mostrarse un texto que vex no escribió (viene de la plantilla del pipeline, de la
// nube o del portal): una sola línea, de a lo sumo limit caracteres y solo con ASCII imprimible. Todo lo demás
// (secuencias ANSI, cambios de dirección del texto, letras de otros alfabetos que se confunden con las latinas) se
// escribe escapado, a la vista: \u{202e}. Así un texto hostil no puede falsificar lo que se lee en la terminal.
func ExternalText(text string, limit int) string {
	var out strings.Builder
	count := 0
	space := false
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			space = out.Len() > 0
			continue
		}
		piece := string(r)
		if r < 0x21 || r > 0x7e {
			piece = fmt.Sprintf(`\u{%x}`, r)
		}
		if space {
			out.WriteByte(' ')
			count++
			space = false
		}
		if count >= limit {
			out.WriteString("…")
			break
		}
		out.WriteString(piece)
		count += len(piece)
	}
	return out.String()
}
