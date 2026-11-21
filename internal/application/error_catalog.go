package application

import (
	"errors"
	"fmt"
	"strings"

	arqPor "github.com/jairoprogramador/vex/internal/domain/architecture/ports"
)

// ActionableError es un error explicado: qué pasó y qué hacer, bajo un código que no cambia entre versiones
// (es lo que se busca en docs/solucion-de-problemas.md).
type ActionableError struct {
	Code    string
	Message string // qué pasó
	Action  string // qué hacer
	// Extra son líneas que acompañan a la acción, p. ej. comandos que quedan por ejecutar.
	Extra []string
	// Cause es el error explicado.
	Cause error
}

func (e *ActionableError) Error() string {
	if e.Action == "" {
		return e.Message
	}
	return e.Message + "\n→ " + e.Action
}

func (e *ActionableError) Unwrap() error { return e.Cause }

// CatalogRule es una entrada del catálogo de errores: reconoce un caso y lo explica.
type CatalogRule struct {
	Code string
	// Explain devuelve qué pasó y qué hacer si err es de este caso.
	Explain func(err error) (message, action string, ok bool)
	// Example es un error de este caso: con él se comprueba que toda regla dice qué hacer.
	Example error
}

// ErrorCatalog lo aporta cada adaptador de nube con los errores que solo él conoce.
type ErrorCatalog interface {
	ErrorRules() []CatalogRule
}

// ErrorRules son las reglas comunes más las de los catálogos dados. El orden importa: gana la primera que
// reconoce el error, y las de los adaptadores (más concretas) van antes.
func ErrorRules(catalogs ...ErrorCatalog) []CatalogRule {
	var rules []CatalogRule
	for _, catalog := range catalogs {
		rules = append(rules, catalog.ErrorRules()...)
	}
	return append(rules, neutralRules...)
}

// ExplainError busca err en el catálogo. Sin regla que lo reconozca devuelve false: el error se muestra tal cual.
func ExplainError(err error, catalogs ...ErrorCatalog) (*ActionableError, bool) {
	if err == nil {
		return nil, false
	}
	for _, rule := range ErrorRules(catalogs...) {
		if message, action, ok := rule.Explain(err); ok {
			return &ActionableError{Code: rule.Code, Message: message, Action: action, Cause: err}, true
		}
	}
	return nil, false
}

// SplitAdvice separa un mensaje «qué pasó\n→ qué hacer» en sus dos partes.
func SplitAdvice(text string) (message, action string) {
	message, action, _ = strings.Cut(text, "\n→ ")
	return message, action
}

// Fixed es una regla para un error centinela con texto fijo.
func Fixed(code string, target error, message, action string) CatalogRule {
	return CatalogRule{
		Code: code, Example: target,
		Explain: func(err error) (string, string, bool) { return message, action, errors.Is(err, target) },
	}
}

// selfExplained es una regla para un error cuyo propio texto ya dice qué pasó (y quizá qué hacer, tras «→»);
// fallback es la acción cuando el texto no trae ninguna.
func selfExplained(code string, target, example error, fallback string) CatalogRule {
	return CatalogRule{
		Code: code, Example: example,
		Explain: func(err error) (string, string, bool) {
			if !errors.Is(err, target) {
				return "", "", false
			}
			message, action := SplitAdvice(err.Error())
			if action == "" {
				action = fallback
			}
			return capitalize(message), action, true
		},
	}
}

// typed es una regla para un tipo de error con datos.
func typed[E error](code string, example E, explain func(E) (message, action string)) CatalogRule {
	return CatalogRule{
		Code: code, Example: example,
		Explain: func(err error) (string, string, bool) {
			var target E
			if !errors.As(err, &target) {
				return "", "", false
			}
			message, action := explain(target)
			return message, action, message != ""
		},
	}
}

const (
	storeIssues   = "https://github.com/jairoprogramador/vex-template-store/issues"
	noCredentials = "No se entregaron credenciales de nube."
)

// neutralRules son los errores que no dependen de ninguna nube. Ninguna regla muestra tokens, cabeceras ni la
// causa técnica cruda: solo los datos que la persona ya conoce (ambiente, imagen, pipeline).
var neutralRules = []CatalogRule{
	// Cadena de suministro: catálogo, plantilla e imagen.
	Fixed("catalogo_firma_invalida", arqPor.ErrCatalogSignature,
		"No pude verificar la firma del catálogo de plantillas.",
		"No se usó. Reintenta en unos minutos; si persiste, avisa en "+storeIssues),
	Fixed("catalogo_retroceso", arqPor.ErrCatalogRollback,
		"El catálogo descargado es más antiguo que el que ya tenías verificado.",
		"No se usó. Si fue un error al publicar, quien mantiene el catálogo debe publicar un release nuevo; avisa en "+storeIssues),
	Fixed("catalogo_no_disponible", arqPor.ErrCatalogUnavailable,
		"No pude descargar el catálogo firmado de plantillas y no hay una copia verificada en este equipo.",
		"Revisa tu conexión a Internet y vuelve a intentarlo. Una plantilla de vex no se usa sin compararla con su catálogo"),
	Fixed("catalogo_copia_antigua", arqPor.ErrCatalogStale,
		"No pude descargar el catálogo firmado de plantillas y la copia de este equipo tiene más de 7 días.",
		"No se usó: una copia tan antigua puede ocultar una plantilla retirada. Revisa tu conexión a Internet y vuelve a intentarlo"),
	Fixed("catalogo_copia_incompleta", arqPor.ErrCatalogCacheBroken,
		"La copia guardada del catálogo de plantillas no tiene su marca de fecha, o está dañada: no puedo comprobar que el catálogo no retrocede.",
		"No se usó. Si no la tocaste tú, revisa quién escribe en ~/.vex. Para empezar de cero, borra ~/.vex/pipelines.json, "+
			"~/.vex/pipelines.json.sigstore.json y ~/.vex/pipelines.json.meta y vuelve a intentarlo"),
	typed("plantilla_no_coincide", &TemplateMismatchError{PipelineURL: "https://github.com/acme/pipe.git", Commit: "0123abc"},
		func(e *TemplateMismatchError) (string, string) {
			return fmt.Sprintf("El bootstrap de %s en el commit %s no es el que publica el catálogo firmado de vex.", e.PipelineURL, e.Commit),
				"No se aplicó ni se ejecutó nada. Ejecuta `vex arq` para volver a la plantilla del catálogo; si no cambiaste nada, avisa en " + storeIssues
		}),
	typed("imagen_sin_firma", &ImageVerificationError{Image: "acme/runtime", Tag: "1.0.0", Reason: ErrImageNotSigned},
		imageRule(ErrImageNotSigned, func(e *ImageVerificationError) (string, string) {
			return fmt.Sprintf("La imagen de runtime «%s» no tiene firma de vex.", e.Image),
				noCredentials + " Usa una imagen oficial (`vex arq` la elige) o construye la tuya con un Dockerfile en runtime.image"
		})),
	typed("imagen_otra_identidad", &ImageVerificationError{Image: "acme/runtime", Reason: ErrImageIdentity, Signer: "https://example.com/otro"},
		imageRule(ErrImageIdentity, func(e *ImageVerificationError) (string, string) {
			return fmt.Sprintf("La imagen «%s» está firmada por «%s», no por el workflow oficial de vex.", e.Image, e.Signer),
				noCredentials + " Si la imagen es tuya, constrúyela con un Dockerfile en runtime.image"
		})),
	typed("imagen_digest_movido", &ImageVerificationError{Image: "acme/runtime", Tag: "1.0.0", Reason: ErrImageDigestMoved},
		imageRule(ErrImageDigestMoved, func(e *ImageVerificationError) (string, string) {
			return fmt.Sprintf("El tag «%s» de «%s» ya no apunta al digest fijado en vexconfig.yaml.", e.Tag, e.Image),
				noCredentials + " Un tag de versión no debería cambiar: revisa el release de la imagen, o ejecuta `vex arq` para tomar la del catálogo"
		})),
	typed("imagen_registro", &ImageVerificationError{Image: "acme/runtime", Reason: ErrImageRegistry},
		imageRule(ErrImageRegistry, func(e *ImageVerificationError) (string, string) {
			return fmt.Sprintf("No pude consultar el registro de la imagen «%s» para verificar su firma.", e.Image),
				noCredentials + " Revisa tu conexión a Internet (y `docker login` si el registro es privado) y vuelve a intentarlo"
		})),

	// Contrato y configuración.
	typed("contrato_no_soportado", &UnsupportedContractError{Required: "otra/v9", Supported: []ContractID{"nube/v1"}},
		func(e *UnsupportedContractError) (string, string) {
			message, _, _ := strings.Cut(e.Error(), ". Actualiza vex")
			return capitalize(message) + ".", "Actualiza vex (`vex version` dice cuál tienes)"
		}),
	selfExplained("config_nube_invalida", ErrCloudConfig,
		fmt.Errorf("%w: cloud.credentials es \"portal\" pero runtime.run.envs define X", ErrCloudConfig),
		"Quita esas variables de vexconfig.yaml, o usa «credentials: env» si quieres tus propios secretos"),
	Fixed("credenciales_portal_no_disponibles", ErrPortalCredentialsUnavailable,
		"vexconfig.yaml pide las credenciales del portal, pero esta compilación de vex no puede obtenerlas.",
		"Instala una versión oficial de vex"),
	typed("nombre_invalido", &InvalidEngineNameError{What: "el ambiente"},
		func(e *InvalidEngineNameError) (string, string) {
			message, action := SplitAdvice(e.Error())
			if action == "" {
				action = "Usa uno de los ambientes del pipeline: `vex envs` los lista"
			}
			return capitalize(message), action
		}),

	// vex bootstrap.
	Fixed("sin_credenciales_portal", ErrBootstrapNeedsPortal,
		"Este proyecto no usa las credenciales del portal, así que no hay nube que conectar.",
		"Para usarlas, pon en vexconfig.yaml: cloud: { credentials: portal }"),
	Fixed("emisor_no_configurado", ErrIssuerNotConfigured,
		"Este binario de vex no trae configurado el emisor de tokens y no puede crear confianzas en tu nube.",
		"Instala una versión oficial de vex"),
	typed("bootstrap_rechazado", &BootstrapRejectedError{Rule: "S1", Detail: "detalle"},
		func(e *BootstrapRejectedError) (string, string) {
			return e.Error(), "No se aplicó nada. La regla está explicada en docs/contrato-plantillas.md; corrige la plantilla del pipeline o avisa a quien la mantiene"
		}),
	selfExplained("bootstrap_no_aceptado", ErrBootstrapNotAccepted, ErrBootstrapNotAccepted,
		"No se aplicó nada. Vuelve a ejecutar `vex bootstrap <ambiente>` y acepta la vista previa"),
	selfExplained("bootstrap_cambio_tras_vista_previa", ErrBootstrapChanged, ErrBootstrapChanged,
		"No se aplicó nada. Vuelve a ejecutar `vex bootstrap <ambiente>` para ver la vista previa de lo que hay ahora"),
	typed("bootstrap_desactualizado", &BootstrapOutdatedError{Environment: "sand", Detail: "detalle"},
		func(e *BootstrapOutdatedError) (string, string) {
			return fmt.Sprintf("El bootstrap del pipeline cambió desde que se conectó '%s' (%s).", e.Environment, e.Detail),
				fmt.Sprintf("Ejecuta `vex bootstrap %s`: muestra los cambios antes de aplicarlos", e.Environment)
		}),
	selfExplained("verificacion_pendiente", ErrVerificationPending, ErrVerificationPending,
		"Lo aplicado en la nube sigue ahí: vuelve a ejecutar `vex bootstrap <ambiente>` para retomarlo"),
	typed("verificacion_fallida", &VerificationFailedError{Environment: "sand", Message: "motivo"},
		func(e *VerificationFailedError) (string, string) {
			return fmt.Sprintf("La conexión de nube de '%s' no pasó la verificación: %s", e.Environment, e.Message),
				bootstrapAdvice(e.Environment, e.Hint)
		}),
	typed("conexion_rechazada", &ConnectionRejectedError{Code: RejectionConnectionExists, Environment: "sand"},
		func(e *ConnectionRejectedError) (string, string) {
			problem, action := e.Explain()
			if action == "" {
				action = "Pide ayuda a quien administra el proyecto en el portal"
			}
			return capitalize(problem) + ".", capitalize(action)
		}),
	Fixed("auditoria_con_problemas", ErrCheckFoundProblems,
		"La conexión no pasó la auditoría (detalle arriba).",
		"Ejecuta `vex bootstrap <ambiente>` para volver a aplicar lo que el pipeline declara"),

	// vex run / rollback con credenciales del portal.
	typed("conexion_no_lista", &ConnectionNotReadyError{Environment: "sand"},
		func(e *ConnectionNotReadyError) (string, string) { return e.Explain() }),
	typed("acceso_nube_perdido", &CloudAccessLostError{Environment: "sand", Step: "deploy", Err: ErrCloudConnectionInactive},
		func(e *CloudAccessLostError) (string, string) { return SplitAdvice(e.Error()) }),
	selfExplained("rollback_sin_ambiente", ErrRollbackEnvironmentUnknown, ErrRollbackEnvironmentUnknown, ""),
	Fixed("sesion_invalida", ErrPortalSessionInvalid,
		"Tu sesión de vex venció o fue cerrada.",
		"Ejecuta `vex login` y vuelve a intentarlo"),

	// vex disconnect.
	selfExplained("sin_conexion", ErrNoCloudConnection, ErrNoCloudConnection,
		"No hay nada que desconectar. `vex bootstrap <ambiente> --check` muestra el estado de la conexión"),
	typed("confianza_bloqueada", &TrustLockedError{},
		func(*TrustLockedError) (string, string) {
			return "Un bloqueo de la nube impide retirar la confianza de vex.",
				"vex no quita bloqueos por su cuenta: ejecuta los comandos que aparecen arriba con tu sesión de la nube"
		}),
}

// imageRule limita una regla de imagen a su motivo: las cuatro comparten tipo.
func imageRule(reason error, explain func(*ImageVerificationError) (string, string)) func(*ImageVerificationError) (string, string) {
	return func(e *ImageVerificationError) (string, string) {
		if !errors.Is(e.Reason, reason) {
			return "", ""
		}
		return explain(e)
	}
}

func capitalize(text string) string {
	if text == "" {
		return text
	}
	runes := []rune(text)
	return strings.ToUpper(string(runes[:1])) + string(runes[1:])
}
