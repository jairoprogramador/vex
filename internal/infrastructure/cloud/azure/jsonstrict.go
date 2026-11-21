package azure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// object es un objeto JSON cuyas claves se buscan sin distinguir mayúsculas, como hace Azure con los nombres de
// las propiedades. parseStrict garantiza que no hay dos claves que solo difieran en eso.
type object map[string]any

// get devuelve el valor de key sin distinguir mayúsculas.
func (o object) get(key string) (any, bool) {
	if value, ok := o[key]; ok {
		return value, true
	}
	for name, value := range o {
		if strings.EqualFold(name, key) {
			return value, true
		}
	}
	return nil, false
}

func (o object) str(key string) (string, bool) {
	value, _ := o.get(key)
	text, ok := value.(string)
	return text, ok
}

func (o object) obj(key string) (object, bool) {
	value, _ := o.get(key)
	child, ok := value.(object)
	return child, ok
}

const maxJSONDepth = 64

// parseStrict lee un único valor JSON. Rechaza lo que haría que vex y Azure pudieran leer cosas distintas del
// mismo documento: claves repetidas (también si solo cambian las mayúsculas), comentarios y datos tras el valor.
func parseStrict(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := parseValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("hay datos después del documento JSON")
	}
	return value, nil
}

func parseValue(dec *json.Decoder, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, errors.New("el JSON está anidado en exceso")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("JSON no válido: %w", err)
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	switch delim {
	case '{':
		out := object{}
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("JSON no válido: %w", err)
			}
			key, _ := keyToken.(string)
			if folded := strings.ToLower(key); seen[folded] {
				return nil, fmt.Errorf("el JSON repite la clave %q", clean(key, 60))
			} else {
				seen[folded] = true
			}
			if out[key], err = parseValue(dec, depth+1); err != nil {
				return nil, err
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("JSON no válido: %w", err)
		}
		return out, nil
	case '[':
		list := []any{}
		for dec.More() {
			item, err := parseValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, item)
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("JSON no válido: %w", err)
		}
		return list, nil
	}
	return nil, errors.New("JSON no válido")
}

// parseObject es parseStrict cuando el documento debe ser un objeto.
func parseObject(data []byte) (object, error) {
	value, err := parseStrict(data)
	if err != nil {
		return nil, err
	}
	doc, ok := value.(object)
	if !ok {
		return nil, errors.New("el JSON no es un objeto")
	}
	return doc, nil
}
