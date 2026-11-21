package console

import (
	"fmt"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// Vistas de `vex bootstrap` y `vex disconnect`. Todo lo que viene de la plantilla del pipeline o de la nube pasa
// por printable antes de escribirse: quien acepta debe poder fiarse de lo que lee.

const highlightMark = "‼"

// external deja un dato que vex no escribió (lo dice el portal o vexconfig.yaml) solo con ASCII imprimible; lo
// demás se ve escapado.
func external(text string) string { return application.ExternalText(text, 200) }

func (v *Views) field(label, value string) {
	v.printf("  %-18s %s\n", label+":", printable(value))
}

// BootstrapPlan pinta lo que hay que revisar antes de aceptar un bootstrap.
func (v *Views) BootstrapPlan(plan *application.BootstrapPlan) {
	switch plan.Mode {
	case application.BootstrapUpToDate:
		v.printf("%s %s ya está conectado con el bootstrap de este pipeline: no hay nada que aplicar.\n", okMark, plan.Environment)
		v.hint("vex bootstrap "+plan.Environment+" --check   (audita la conexión)",
			"vex bootstrap "+plan.Environment+" --reconnect   (la rehace)")
		return
	case application.BootstrapResume:
		v.printf("La conexión de %s ya se aplicó y quedó verificándose: se retoma la verificación.\n", plan.Environment)
		return
	}

	title := "Conectar"
	if plan.Mode == application.BootstrapReconnect {
		title = "Reconectar"
	}
	v.printf("%s la nube de %s con vex (contrato %s)\n\n", title, plan.Environment, plan.Contract)
	for _, line := range plan.Account {
		v.field(line.Label, line.Value)
	}
	v.field("Proyecto", external(plan.ProjectName)+" ("+ownerLabel(plan.Owner)+")")
	if !plan.IsOwner {
		v.printf("  %s Este proyecto pertenece a otra persona: lo que aceptes le da acceso a tu suscripción.\n", warnMark)
	}
	v.field("Pipeline", external(plan.PipelineURL)+" @ "+external(plan.PipelineCommit))
	v.field("Bootstrap", "sha256 "+plan.Fingerprint)
	if plan.Origin == application.TemplateSigned {
		v.field("Plantilla", "es la que publica el catálogo firmado de vex para este commit")
	}
	v.field("Región", plan.Region)
	v.field("Pasos con token", strings.Join(plan.CloudSteps, ", "))
	if len(plan.Providers) > 0 {
		v.field("Servicios", strings.Join(plan.Providers, ", "))
	}
	if plan.Resumed {
		v.printf("  (se retoma una conexión que ya estaba abierta en el portal)\n")
	}
	if plan.Origin != application.TemplateSigned {
		v.printf("\n%s PLANTILLA SIN FIRMA DE VEX: LA REVISAS TÚ.\n"+
			"  El catálogo firmado de vex no respalda este pipeline en este commit. Nadie más revisó lo que su bootstrap\n"+
			"  concede: lee la lista de abajo entera antes de aceptar.\n", warnMark)
	}
	if plan.Mode == application.BootstrapReconnect {
		v.printf("\n%s RECONECTAR CORTA EL ACCESO DE %s HASTA QUE LA CONEXIÓN NUEVA SE VERIFIQUE.\n"+
			"  Al aplicar, la confianza «vex-oidc» pasa de inmediato a la conexión nueva y la anterior deja de obtener\n"+
			"  tokens: mientras dura la verificación (normalmente menos de un minuto; hasta cinco) los `vex run` de este\n"+
			"  ambiente fallan. Si la verificación no pasa, siguen fallando hasta que `vex bootstrap %s` termine bien.\n"+
			"  No lo hagas en mitad de un despliegue.\n", warnMark, strings.ToUpper(printable(plan.Environment)), printable(plan.Environment))
	}

	v.declaredGrants(plan.Preview.Declared)
	v.previewChanges(plan.Preview.Changes)
	v.printf("\nDespués de aplicar, vex crea la credencial federada «vex-oidc» sobre la identidad de despliegue: es lo\n" +
		"único que vex añade por su cuenta. Quien acepta responde de todo lo que esta lista concede.\n")
	if elevated := plan.Preview.ElevatedGrants(); len(elevated) > 0 {
		v.printf("\n%s %d permiso(s) resaltado(s): dan control sobre permisos, alcanzan más que un grupo de recursos, son para una\n"+
			"  identidad o sobre algo que este bootstrap no crea, o no se pueden determinar sin desplegar. Acéptalos solo si\n"+
			"  sabes por qué el pipeline los necesita.\n", highlightMark, len(elevated))
	}
	if blind := plan.Preview.Unpreviewed(); len(blind) > 0 {
		v.printf("\n%s %d recurso(s) sin vista previa: aceptar es aceptar a ciegas lo que pase con ellos.\n", highlightMark, len(blind))
	}
}

// TrustExposure pinta, con la plantilla ya aplicada y antes de crear la confianza, los permisos que la identidad
// de despliegue tiene sin que la vista previa los mostrara.
func (v *Views) TrustExposure(environment string, grants []application.PlannedGrant) {
	v.printf("\n%s LA IDENTIDAD DE DESPLIEGUE TIENE PERMISOS QUE LA VISTA PREVIA NO MOSTRÓ.\n"+
		"  Si aceptas, vex crea su confianza sobre ella y cada `vex run` de %s podrá usar estos permisos:\n",
		warnMark, printable(environment))
	for _, grant := range grants {
		v.printf("  %s\n", grantLine(grant))
	}
	v.printf("  Si no los reconoces, no aceptes: la plantilla ya se aplicó, pero sin la confianza vex no puede usarlos.\n")
}

func ownerLabel(owner application.ProjectOwner) string {
	switch {
	case owner.Name == "" && owner.Email == "":
		return "sin dueño conocido"
	case owner.Email == "":
		return "dueño: " + owner.Name
	case owner.Name == "":
		return "dueño: " + owner.Email
	}
	return "dueño: " + owner.Name + " <" + owner.Email + ">"
}

func (v *Views) declaredGrants(grants []application.PlannedGrant) {
	if len(grants) == 0 {
		v.printf("\nLa plantilla no declara permisos.\n")
		return
	}
	v.printf("\nPermisos que declara la plantilla (identidad → rol → ámbito):\n")
	for _, grant := range grants {
		v.printf("  %s\n", grantLine(grant))
	}
}

func grantLine(grant application.PlannedGrant) string {
	line := printable(grant.Identity + " → " + grant.Role + " → " + grant.Scope)
	if !grant.Elevated {
		return "• " + line
	}
	return highlightMark + " " + line + "   [RESALTADO: " + printable(grant.Reason) + "]"
}

func (v *Views) previewChanges(changes []application.PlannedChange) {
	v.printf("\nVista previa de la nube:\n")
	labels := map[application.ChangeAction]string{application.ChangeCreate: "+ crear    ", application.ChangeModify: "~ modificar"}
	unchanged, shown := 0, 0
	var blind []application.PlannedChange
	for _, change := range changes {
		switch change.Action {
		case application.ChangeNone:
			unchanged++
		case application.ChangeIgnored, application.ChangeUnknown:
			blind = append(blind, change)
		default:
			shown++
			if change.Grant != nil {
				v.printf("  %s permiso: %s\n", labels[change.Action], grantLine(*change.Grant))
				continue
			}
			v.printf("  %s %s  %s\n", labels[change.Action], printable(change.Kind), printable(change.Name))
		}
	}
	if shown == 0 {
		v.printf("  (nada que crear ni modificar)\n")
	}
	if unchanged > 0 {
		v.printf("  = sin cambios: %d recurso(s)\n", unchanged)
	}
	if len(blind) == 0 {
		return
	}
	v.printf("\nNo se pudo previsualizar (%d): la nube no dice qué pasará con estos recursos.\n", len(blind))
	for _, change := range blind {
		note := change.Note
		if change.Action == application.ChangeIgnored {
			note = "ya existe y la nube lo ignora"
		}
		v.printf("  ? %s  %s — %s\n", printable(change.Kind), printable(change.Name), printable(note))
	}
}

// BootstrapDone confirma que el ambiente quedó conectado.
func (v *Views) BootstrapDone(result application.BootstrapResult) {
	v.printf("%s Conexión de %s activa y verificada (%s).\n", okMark, result.Environment, elapsed(result.Elapsed))
	v.hint("vex run <paso> "+result.Environment, "vex bootstrap "+result.Environment+" --check")
}

// CheckReport pinta la auditoría punto por punto.
func (v *Views) CheckReport(report application.CheckReport) {
	v.printf("Auditoría de la conexión de %s (%s)\n", report.Environment, report.Contract)
	for _, line := range report.Account {
		v.field(line.Label, line.Value)
	}
	marks := map[application.CheckStatus]string{
		application.CheckOK: okMark, application.CheckFailed: failMark, application.CheckWarning: warnMark,
	}
	for _, item := range report.Items {
		v.printf("%s %s\n", marks[item.Status], printable(item.Text))
	}
	if report.Failed() {
		v.printf("\nLa conexión no está como vex la dejó. No se cambió nada.\n")
		return
	}
	v.printf("\nTodo en orden. No se cambió nada.\n")
}

// DisconnectWarning pinta lo que va a pasar al desconectar, antes de pedir confirmación.
func (v *Views) DisconnectWarning(plan *application.DisconnectPlan) {
	v.printf("Vas a desconectar la nube de %s (conexión %s).\n", plan.Environment, printable(plan.Status))
	v.printf("  1. vex revoca la conexión en el portal: deja de emitir tokens para este ambiente.\n")
	v.printf("  2. vex borra su credencial federada de la identidad de despliegue, con tu sesión de la nube.\n")
	v.printf("  3. No se borra ningún recurso ni permiso: al final verás los comandos para hacerlo tú.\n")
	v.printf("  Ojo: el corte no es instantáneo. Un token ya emitido vale hasta 10 minutos, y un token de la nube ya\n" +
		"  canjeado con él, hasta que venza (alrededor de una hora).\n")
}

// Disconnected pinta hasta dónde llegó la desconexión y lo que queda en manos de la persona.
func (v *Views) Disconnected(result application.DisconnectResult) {
	if result.TrustRemoved {
		v.printf("%s %s desconectado: conexión revocada y confianza de vex retirada de la nube.\n", okMark, result.Environment)
	} else {
		v.printf("%s %s quedó revocado en el portal, pero la confianza de vex sigue en la nube: %s.\n",
			warnMark, result.Environment, printable(result.Pending))
		v.commands("Para retirarla tú:", result.Manual)
	}
	v.commands("vex no borra recursos ni permisos: son tuyos. Para eliminar lo que creó el bootstrap:", result.Cleanup)
	v.printf("Recuerda: un token ya emitido puede seguir valiendo unos minutos (hasta 10 el de vex; más el de la nube ya canjeado).\n")
}

func (v *Views) commands(title string, commands []string) {
	if len(commands) == 0 {
		return
	}
	v.printf("\n%s\n", title)
	for _, command := range commands {
		v.printf("  %s\n", printable(command))
	}
	fmt.Fprintln(v.out)
}
