package console

import (
	"slices"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// unknownEnvironment ocupa el lugar del ambiente en una instrucción cuando no se sabe cuál fue.
const unknownEnvironment = "<ambiente>"

// CloudHints devuelve las instrucciones que corresponden al texto de un comando fallido, en el orden de hints y
// sin repetir. Qué errores se reconocen y qué se aconseja lo decide el adaptador de cada nube; aquí solo se
// aplican. {env} se sustituye por el ambiente.
func CloudHints(output string, hints []application.ErrorHint, environment string) []string {
	if environment == "" {
		environment = unknownEnvironment
	}
	var messages []string
	for _, hint := range hints {
		if hint.Pattern == nil || !hint.Pattern.MatchString(output) {
			continue
		}
		message := strings.ReplaceAll(hint.Message, "{env}", environment)
		if !slices.Contains(messages, message) {
			messages = append(messages, message)
		}
	}
	return messages
}

// hintPrinter imprime las instrucciones de varios comandos sin repetir una ya mostrada.
type hintPrinter struct {
	hints       []application.ErrorHint
	environment string
	shown       []string
}

// next devuelve las instrucciones nuevas para la salida de un comando.
func (p *hintPrinter) next(output string) []string {
	var fresh []string
	for _, message := range CloudHints(output, p.hints, p.environment) {
		if !slices.Contains(p.shown, message) {
			p.shown = append(p.shown, message)
			fresh = append(fresh, message)
		}
	}
	return fresh
}
