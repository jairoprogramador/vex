package console

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShortID(t *testing.T) {
	assert.Equal(t, "5ab306c", ShortID("01a106d1-94e1-727c-a252-4efac5ab306c"))
	assert.Equal(t, "abc", ShortID("abc"), "un id corto se deja tal cual")
	assert.Equal(t, "", ShortID(""))
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"hace segundos", now.Add(-20 * time.Second), "hace unos segundos"},
		{"minutos", now.Add(-5 * time.Minute), "hace 5 min"},
		{"horas", now.Add(-3 * time.Hour), "hace 3 h"},
		{"ayer", now.Add(-30 * time.Hour), "ayer"},
		{"días", now.Add(-72 * time.Hour), "hace 3 d"},
		{"fecha si pasó más de un mes", now.Add(-60 * 24 * time.Hour), now.Add(-60 * 24 * time.Hour).Local().Format("2 Jan 2006")},
		{"sin fecha", time.Time{}, "—"},
		{"en el futuro (relojes distintos)", now.Add(time.Minute), "ahora"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Ago(now, tt.t))
		})
	}
}

func TestDuration(t *testing.T) {
	assert.Equal(t, "320 ms", Duration(320*time.Millisecond))
	assert.Equal(t, "12 s", Duration(12*time.Second+300*time.Millisecond))
	assert.Equal(t, "1 min 5 s", Duration(65*time.Second))
	assert.Equal(t, "2 min", Duration(2*time.Minute))
	assert.Equal(t, "1 h 30 min", Duration(90*time.Minute))
	assert.Equal(t, "2 h", Duration(2*time.Hour))
}

func TestTable_AlineaPorCaracteresVisibles(t *testing.T) {
	var out bytes.Buffer

	Table(&out, [][]string{
		{"ID", "RESULTADO", "QUIÉN"},
		{"5ab306c", "✔ exitoso", "jailux"},
		{"efac5ab", "✘ fallido", "ana"},
	})

	assert.Equal(t, "ID       RESULTADO  QUIÉN\n"+
		"5ab306c  ✔ exitoso  jailux\n"+
		"efac5ab  ✘ fallido  ana\n", out.String())
}

func TestTable_LosColoresNoDescuadranLasColumnas(t *testing.T) {
	green, reset := "\x1b[32m", "\x1b[0m"
	var out bytes.Buffer

	Table(&out, [][]string{
		{"ID", "RESULTADO", "QUIÉN"},
		{"5ab306c", green + "✔" + reset + " exitoso", "jailux"},
		{"efac5ab", "… en curso", "ana"},
	})

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	column := func(line string) int { return visibleWidth(line[:strings.LastIndex(line, "  ")+2]) }
	assert.Equal(t, column(lines[0]), column(lines[1]), "la tercera columna empieza en el mismo sitio con y sin color")
	assert.Equal(t, column(lines[0]), column(lines[2]))
}

func TestTable_UnaUltimaCeldaVaciaNoDejaEspaciosAlFinal(t *testing.T) {
	var out bytes.Buffer

	Table(&out, [][]string{{"1", "test", ""}, {"2", "acr", "compartido"}})

	assert.Equal(t, "1  test\n2  acr   compartido\n", out.String())
}

func TestTable_SinFilasNoEscribeNada(t *testing.T) {
	var out bytes.Buffer

	Table(&out, nil)

	assert.Empty(t, out.String())
}

func TestIndentYPlural(t *testing.T) {
	assert.Equal(t, "  a\n\n  b", Indent("a\n\nb\n", "  "), "las líneas vacías no se sangran")
	assert.Equal(t, "1 intento", Plural(1, "intento", "intentos"))
	assert.Equal(t, "3 intentos", Plural(3, "intento", "intentos"))
}
