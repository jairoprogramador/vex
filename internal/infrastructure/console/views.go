package console

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
)

// Views pinta lo que responden los comandos de consulta. Todas las vistas siguen la misma forma: la primera línea
// dice el resultado, después van los hechos y al final, el siguiente comando concreto (→).
type Views struct {
	out io.Writer
	now func() time.Time
}

func NewViews(out io.Writer) *Views {
	return &Views{out: out, now: time.Now}
}

// WithClock cambia el reloj con el que se calcula «hace 5 min»; sirve para probar con salidas exactas.
func (v *Views) WithClock(now func() time.Time) *Views {
	v.now = now
	return v
}

func (v *Views) printf(format string, args ...any) { fmt.Fprintf(v.out, format, args...) }

func (v *Views) hint(commands ...string) {
	if len(commands) > 0 {
		v.printf("→ %s\n", strings.Join(commands, " · "))
	}
}

// --- Intentos ---

// attemptLabel es el resultado de un intento en pocas palabras, con su símbolo.
func attemptSummaryLabel(a application.AttemptSummary) string {
	switch {
	case a.Abandoned:
		return "– abandonado"
	case a.InProgress():
		return "… en curso"
	case a.Status == application.AttemptSucceeded:
		return okMark + " exitoso"
	case a.Status == application.AttemptCanceled:
		return "– cancelado"
	case a.Cause == application.CauseInterrupted:
		return warnMark + " interrumpido"
	}
	return failMark + " fallido"
}

// AttemptList pinta `vex ls <ambiente>`.
func (v *Views) AttemptList(list application.AttemptList) {
	if len(list.Attempts) == 0 {
		v.printf("%s: todavía no hay intentos.\n", list.Environment)
		v.hint(fmt.Sprintf("Ejecuta uno con: vex <paso> %s", list.Environment))
		return
	}

	v.printf("%s · %s\n", list.Environment, listTitle(len(list.Attempts), list.Total, "intento", "intentos"))
	rows := [][]string{{"ID", "HASTA", "RESULTADO", "QUIÉN", "CUÁNDO"}}
	for _, a := range list.Attempts {
		rows = append(rows, []string{
			ShortID(a.ID), a.UntilStep, attemptSummaryLabel(a), a.Requester, Ago(v.now(), a.StartedAt),
		})
	}
	Table(v.out, rows)

	// La pista apunta al fallo más reciente que merece investigarse (no a un interrumpido); si no hay ninguno,
	// al último intento, y entonces solo se ofrece verlo: `why` no tendría nada que explicar.
	for _, a := range list.Attempts {
		if a.Status == application.AttemptFailed && a.Cause != application.CauseInterrupted && !a.Abandoned {
			v.hint("vex show "+ShortID(a.ID), "vex why "+ShortID(a.ID))
			return
		}
	}
	v.hint("vex show " + ShortID(list.Attempts[0].ID))
}

// listTitle: «últimos 4 intentos» o «últimos 10 de 37 intentos».
func listTitle(shown, total int, singular, plural string) string {
	if shown == total {
		if shown == 1 {
			return "1 " + singular
		}
		return fmt.Sprintf("últimos %d %s", shown, plural)
	}
	return fmt.Sprintf("últimos %d de %d %s", shown, total, plural)
}

// AttemptDetail pinta `vex show [intento]`.
func (v *Views) AttemptDetail(d application.AttemptDetail) {
	v.printf("Intento %s · %s · hasta %s\n", ShortID(d.ID), d.Environment, d.UntilStep)
	v.printf("  %s · %s%s\n", d.Requester, Ago(v.now(), d.StartedAt), durationSuffix(d))
	v.printf("%s\n", attemptHeadline(d))
	if d.RolledBackTo != "" {
		v.printf("  ↩ volvió al despliegue %s\n", ShortID(d.RolledBackTo))
	}

	rows := make([][]string, 0, len(d.Steps))
	for _, s := range d.Steps {
		rows = append(rows, []string{"  " + stepMark(s.Status) + " " + s.Name, stepOutcomeLabel(s.Status)})
	}
	Table(v.out, rows)
	v.hint(attemptHints(d)...)
}

func durationSuffix(d application.AttemptDetail) string {
	if d.InProgress() || d.Duration <= 0 {
		return ""
	}
	return " · duró " + Duration(d.Duration)
}

// attemptHeadline resume cómo terminó el intento y, si falló, dónde.
func attemptHeadline(d application.AttemptDetail) string {
	switch {
	case d.Abandoned:
		return warnMark + " abandonado: se dio por perdido a mano"
	case d.InProgress():
		return "… en curso (si no avanza, su proceso pudo morir: el motor lo recupera solo al lanzar otro intento)"
	case d.Status == application.AttemptSucceeded:
		return okMark + " exitoso"
	case d.Status == application.AttemptCanceled:
		return "– cancelado"
	case d.Cause == application.CauseInterrupted:
		return warnMark + " interrumpido: su proceso murió sin terminar" + inStep(d)
	case d.Cause == application.CauseError:
		return failMark + " se detuvo por un error al ejecutar (no por un comando)" + inStep(d)
	}
	return failMark + " fallido" + inStep(d)
}

func inStep(d application.AttemptDetail) string {
	for _, s := range d.Steps {
		if s.Status == application.StepFailed || s.Status == application.StepUnfinished {
			return " en «" + s.Name + "»"
		}
	}
	return ""
}

func attemptHints(d application.AttemptDetail) []string {
	short := ShortID(d.ID)
	retry := fmt.Sprintf("vex %s %s", d.UntilStep, d.Environment)
	switch {
	case d.InProgress() || d.Abandoned:
		return nil
	case d.Status == application.AttemptSucceeded:
		return []string{"vex log " + short}
	case d.Status == application.AttemptCanceled || d.Cause == application.CauseInterrupted:
		return []string{"vex log " + short, "reintentar: " + retry}
	}
	return []string{"vex log " + short + " --failed", "vex why " + short}
}

func stepMark(s application.StepStatus) string {
	switch s {
	case application.StepExecuted, application.StepReused:
		return okMark
	case application.StepFailed:
		return failMark
	case application.StepUnfinished:
		return warnMark
	case application.StepCanceled:
		return "–"
	}
	return "·"
}

func stepOutcomeLabel(s application.StepStatus) string {
	switch s {
	case application.StepExecuted:
		return "ejecutado"
	case application.StepReused:
		return "reutilizado"
	case application.StepFailed:
		return "falló"
	case application.StepUnfinished:
		return "no terminó"
	case application.StepCanceled:
		return "cancelado"
	}
	return "no se llegó a él"
}

// Logs pinta `vex log [intento]`.
func (v *Views) Logs(logs application.AttemptLogs, onlyFailed bool) {
	subject := "salida de los comandos"
	if onlyFailed {
		subject = "salida de los comandos fallidos"
	}
	if logs.AttemptID != "" {
		v.printf("Intento %s · %s\n", ShortID(logs.AttemptID), subject)
	} else {
		v.printf("Último intento · %s\n", subject)
	}

	if len(logs.Outputs) == 0 {
		if onlyFailed {
			v.printf("Ningún comando falló.\n")
			v.hint("vex log (sin --failed) para ver toda la salida")
			return
		}
		v.printf("No hay salida guardada: los pasos reutilizados no ejecutan comandos.\n")
		return
	}
	for _, output := range logs.Outputs {
		mark := okMark
		if !output.Succeeded {
			mark = failMark
		}
		v.printf("%s %s › %s\n", mark, output.Step, output.Command)
		if text := strings.TrimRight(output.Text, "\n"); text != "" {
			v.printf("%s\n", Indent(text, "  "))
		}
	}
}

// --- Despliegues y lanzamientos ---

// releaseName es el nombre con que se muestra un lanzamiento: `v1` si el nombre es solo el número de versión.
func releaseName(r application.Release) string {
	if r.Name == strconv.Itoa(r.Version) {
		return "v" + r.Name
	}
	return r.Name
}

// DeploymentList pinta `vex deployments <ambiente>`.
func (v *Views) DeploymentList(list application.DeploymentList) {
	if len(list.Items) == 0 {
		v.printf("%s: todavía no hay despliegues.\n", list.Environment)
		v.printf("  Un despliegue nace cuando un intento ejecuta TODOS los pasos del pipeline con éxito.\n")
		v.hint(fmt.Sprintf("vex <último paso> %s", list.Environment))
		return
	}

	v.printf("%s · %s\n", list.Environment, listTitle(len(list.Items), list.Total, "despliegue", "despliegues"))
	rows := [][]string{{"ID", "CUÁNDO", "INTENTO", "LANZAMIENTO"}}
	for _, item := range list.Items {
		rows = append(rows, []string{
			ShortID(item.Deployment.ID), Ago(v.now(), item.Deployment.At), ShortID(item.Deployment.AttemptID),
			deploymentReleaseLabel(item),
		})
	}
	Table(v.out, rows)
	v.hint("vex rollback <id>", fmt.Sprintf("vex release %s <id>", list.Environment))
}

func deploymentReleaseLabel(item application.DeploymentView) string {
	switch {
	case item.Release == nil:
		return "—"
	case item.Current:
		return "● " + releaseName(*item.Release) + " (actual)"
	}
	return releaseName(*item.Release)
}

// ReleaseList pinta `vex releases <ambiente>`.
func (v *Views) ReleaseList(list application.ReleaseList) {
	if len(list.Releases) == 0 {
		v.printf("%s: todavía no se ha lanzado nada.\n", list.Environment)
		v.hint(fmt.Sprintf("vex release %s <despliegue>", list.Environment), fmt.Sprintf("vex deployments %s", list.Environment))
		return
	}

	v.printf("%s · %s\n", list.Environment, listTitle(len(list.Releases), list.Total, "lanzamiento", "lanzamientos"))
	rows := [][]string{{"LANZAMIENTO", "VERSIÓN", "DESPLIEGUE", "CUÁNDO"}}
	for i, r := range list.Releases {
		name := releaseName(r)
		if i == 0 {
			name = "● " + name + " (actual)"
		}
		rows = append(rows, []string{name, strconv.Itoa(r.Version), ShortID(r.DeploymentID), Ago(v.now(), r.At)})
	}
	Table(v.out, rows)
	v.hint(fmt.Sprintf("vex deployments %s", list.Environment))
}

// --- Pipeline ---

// Environments pinta `vex envs`.
func (v *Views) Environments(environments []application.Environment) {
	if len(environments) == 0 {
		v.printf("El pipeline no declara ningún ambiente.\n")
		return
	}

	v.printf("Ambientes del pipeline, en su orden:\n")
	rows := [][]string{{"  VALOR", "NOMBRE", "LANZAMIENTO", "DESCRIPCIÓN"}}
	anyProtected := false
	for _, e := range environments {
		release := "automático"
		if e.Protected {
			release, anyProtected = "manual (protegido)", true
		}
		rows = append(rows, []string{"  " + e.Value, e.Name, release, e.Description})
	}
	Table(v.out, rows)
	if anyProtected {
		v.hint("en los protegidos el lanzamiento lo decides tú: vex release <ambiente> <despliegue>")
		return
	}
	v.hint("vex <step> <environment> ejecuta en uno de ellos")
}

// Steps pinta `vex steps`. reserved son los nombres de comandos de vex: un paso que se llame igual queda tapado
// por el comando, y se avisa de cómo ejecutarlo.
func (v *Views) Steps(steps []application.PipelineStep, reserved map[string]bool) {
	if len(steps) == 0 {
		v.printf("El pipeline no declara ningún paso.\n")
		return
	}

	v.printf("Pasos del pipeline, en orden:\n")
	rows := make([][]string, 0, len(steps))
	for _, s := range steps {
		scope := ""
		if s.Shared {
			scope = "compartido entre ambientes"
		}
		rows = append(rows, []string{fmt.Sprintf("  %d", s.Order), s.Name, scope})
	}
	Table(v.out, rows)

	for _, s := range steps {
		if reserved[s.Name] {
			v.printf("%s El paso «%s» se llama igual que un comando de vex: ejecútalo con `vex run %s <ambiente>`.\n",
				warnMark, s.Name, s.Name)
		}
	}
	v.hint("vex <step> <environment> ejecuta hasta ese paso (y los anteriores)")
}

// CheckPassed pinta el resultado de `vex check` cuando todo está en orden.
func (v *Views) CheckPassed(environment, step string) {
	v.printf("%s Todo en orden: el pipeline es válido y las variables de %s hasta «%s» se resuelven.\n", okMark, environment, step)
	v.printf("  No se ejecutó nada.\n")
}

// --- Diagnóstico y abandono ---

// Why pinta `vex why [intento]`.
func (v *Views) Why(report application.WhyReport) {
	short := ShortID(report.AttemptID)
	switch report.Outcome {
	case application.WhyDiagnosed:
		v.whyDiagnosed(report, short)
	case application.WhyNoReference:
		v.printf("¿Por qué falló %s%s?\n", short, inEnvironment(report.Environment))
		v.printf("No hay ninguna ejecución anterior que funcionara%s con la que comparar.\n", inEnvironment(report.Environment))
		v.printf("  Hace falta un despliegue exitoso previo para saber qué cambió.\n")
		v.hint("vex log " + short + " --failed para ver el error")
	case application.WhyNotAFailure:
		v.printf("El intento %s terminó bien: no hay nada que diagnosticar.\n", short)
		v.hint("vex show " + short)
	case application.WhyCanceled:
		v.printf("El intento %s se canceló: una cancelación no es un fallo, no hay nada que diagnosticar.\n", short)
		v.hint("vex show " + short)
	case application.WhyInterrupted:
		v.printf("El intento %s se interrumpió (su proceso murió): no falló por un cambio, no hay nada que diagnosticar.\n", short)
		v.hint("vex show " + short)
	case application.WhyInProgress:
		v.printf("El intento %s no ha terminado: todavía no hay un fallo que diagnosticar.\n", short)
		v.hint("vex show " + short)
	default:
		v.printf("El motor no atribuye una causa al intento %s (se canceló, se interrumpió o se abandonó).\n", short)
		v.hint("vex show " + short)
	}
}

func inEnvironment(environment string) string {
	if environment == "" {
		return ""
	}
	return " en " + environment
}

func (v *Views) whyDiagnosed(report application.WhyReport, short string) {
	d := report.Diagnosis
	v.printf("¿Por qué falló %s%s?\n", short, inEnvironment(report.Environment))
	v.printf("Se compara con la última vez que funcionó: el intento %s (%s).", ShortID(d.Reference.ID), Ago(v.now(), d.Reference.At))
	switch {
	case d.AttemptsSince == 1:
		v.printf(" Este es el primer intento desde entonces.\n")
	case d.AttemptsSince > 1:
		v.printf(" Desde entonces se han hecho %d intentos, este incluido.\n", d.AttemptsSince)
	default:
		v.printf("\n")
	}

	if d.Changes.Nothing() {
		v.printf("No cambió nada de lo que mira cada paso: el fallo no viene de un cambio en el pipeline.\n")
		v.hint("revisa lo externo (credenciales, red, servicios) con: vex log " + short + " --failed")
		return
	}
	v.printf("Desde entonces cambió:\n")
	v.changeLine("el código", d.Changes.Code)
	v.changeLine("las instrucciones", d.Changes.Instructions)
	v.variableLine("variables declaradas", d.Changes.DeclaredVars)
	v.variableLine("variables producidas", d.Changes.ProducedVars)
	v.hint("vex log "+short+" --failed", "vex show "+short)
}

func (v *Views) changeLine(label string, steps []string) {
	if len(steps) == 0 {
		return
	}
	word := "paso"
	if len(steps) > 1 {
		word = "pasos"
	}
	v.printf("  • %-22s → %s %s\n", label, word, strings.Join(steps, ", "))
}

func (v *Views) variableLine(label string, variables []application.VariableRef) {
	if len(variables) == 0 {
		return
	}
	names := make([]string, len(variables))
	for i, variable := range variables {
		names[i] = variable.Step + "." + variable.Name
	}
	v.printf("  • %-22s → %s\n", label, strings.Join(names, ", "))
}

// AbandonWarning pinta lo que va a pasar al abandonar, antes de pedir confirmación.
func (v *Views) AbandonWarning(plan application.AbandonPlan) {
	a := plan.Attempt
	v.printf("Intento %s · %s · %s · sin terminar\n", ShortID(a.ID), a.Environment, Ago(v.now(), a.StartedAt))
	v.printf("Abandonarlo libera el ambiente %s para nuevas ejecuciones, pero NO detiene comandos que su proceso siga ejecutando.\n", a.Environment)
}

// Abandoned pinta el resultado de abandonar.
func (v *Views) Abandoned(plan application.AbandonPlan) {
	a := plan.Attempt
	v.printf("%s Intento %s abandonado: el ambiente %s queda libre.\n", okMark, ShortID(a.ID), a.Environment)
	v.hint(fmt.Sprintf("vex <paso> %s", a.Environment))
}

// RollbackWarning pinta lo que va a pasar al volver a un despliegue, antes de pedir confirmación.
func (v *Views) RollbackWarning(plan application.RollbackPlan) {
	short := ShortID(plan.DeploymentID)
	when := ""
	if !plan.DeployedAt.IsZero() {
		when = " (" + Ago(v.now(), plan.DeployedAt) + ")"
	}
	if plan.Environment != "" {
		v.printf("Volverás a desplegar en %s los mismos commits del despliegue %s%s.\n", plan.Environment, short, when)
	} else {
		v.printf("Volverás a desplegar los mismos commits del despliegue %s%s, en el ambiente en que se hizo.\n", short, when)
	}
	v.printf("Se ejecutarán TODOS los pasos del pipeline.\n")
}

// Released confirma un lanzamiento.
func (v *Views) Released(release application.Release) {
	label := fmt.Sprintf("versión %d", release.Version)
	if release.Name != "" {
		label = fmt.Sprintf("«%s» (versión %d)", release.Name, release.Version)
	}
	v.printf("%s %s: lanzado el despliegue %s como %s.\n", okMark, release.Environment, ShortID(release.DeploymentID), label)
	v.hint("vex deployments " + release.Environment)
}

// Protected confirma una reserva y aclara lo que NO hace: no impide desplegar.
func (v *Views) Protected(environment string) {
	v.printf("%s %s protegido: sus despliegues ya no se lanzan solos.\n", okMark, environment)
	v.printf("  Ojo: esto NO impide desplegar ni hacer rollback; solo el lanzamiento automático.\n")
	v.hint(fmt.Sprintf("vex release %s <despliegue>", environment))
}

// Unprotected confirma que el ambiente vuelve al lanzamiento automático.
func (v *Views) Unprotected(environment string) {
	v.printf("%s %s ya no está protegido: cada despliegue exitoso se lanza solo.\n", okMark, environment)
	v.hint("vex envs")
}
