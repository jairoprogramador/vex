package console

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
)

func connectPlan() *application.BootstrapPlan {
	return &application.BootstrapPlan{
		Environment: "sand", Mode: application.BootstrapConnect, Contract: "azure/v1",
		Account: []application.LabeledValue{
			{Label: "Cuenta de Azure", Value: "ana@ejemplo.com"}, {Label: "Suscripción", Value: "Pruebas (2222)"},
		},
		ProjectName: "Pagos API", Owner: application.ProjectOwner{Name: "Ana Pérez", Email: "ana@ejemplo.com"}, IsOwner: true,
		PipelineURL: "https://github.com/local/pipe.git", PipelineCommit: "0123abc", Fingerprint: "c0c0c0",
		CloudSteps: []string{"supply", "deploy"}, Providers: []string{"Microsoft.App"}, Region: "eastus2",
		Preview: application.BootstrapPreview{
			Declared: []application.PlannedGrant{{Identity: "deployPrincipalId", Role: "Contributor", Scope: "grupo de recursos rg-<baseName>"}},
			Changes: []application.PlannedChange{
				{Action: application.ChangeCreate, Kind: "Microsoft.Resources/resourceGroups", Name: "rg-pagos-sand"},
				{Action: application.ChangeCreate, Grant: &application.PlannedGrant{Identity: "identidad 3333", Role: "Contributor", Scope: "grupo de recursos rg-pagos-sand"}},
				{Action: application.ChangeNone, Kind: "x", Name: "y"},
				{Action: application.ChangeUnknown, Kind: "Microsoft.Storage/storageAccounts", Name: "stpendiente", Note: "no se pudo evaluar"},
			},
		},
	}
}

func TestViews_BootstrapPlan_MuestraQuienDondeYQue(t *testing.T) {
	views, out := newTestViews()

	views.BootstrapPlan(connectPlan())

	text := out.String()
	for _, want := range []string{
		"Conectar la nube de sand con vex (contrato azure/v1)",
		"ana@ejemplo.com", "Pruebas (2222)",
		"Pagos API (dueño: Ana Pérez <ana@ejemplo.com>)",
		"https://github.com/local/pipe.git @ 0123abc", "sha256 c0c0c0", "eastus2", "supply, deploy", "Microsoft.App",
		"Permisos que declara la plantilla (identidad → rol → ámbito):",
		"• deployPrincipalId → Contributor → grupo de recursos rg-<baseName>",
		"+ crear     Microsoft.Resources/resourceGroups  rg-pagos-sand",
		"+ crear     permiso: • identidad 3333 → Contributor → grupo de recursos rg-pagos-sand",
		"= sin cambios: 1 recurso(s)",
		"No se pudo previsualizar (1)",
		"? Microsoft.Storage/storageAccounts  stpendiente — no se pudo evaluar",
		"vex crea la credencial federada «vex-oidc»",
	} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "RESALTADO")
	assert.NotContains(t, text, "pertenece a otra persona")
	assert.NotContains(t, text, "RECONECTAR CORTA")
}

// Una plantilla que el catálogo firmado no respalda no puede pasar inadvertida; una que sí, lo dice.
func TestViews_BootstrapPlan_DiceQuienRespondeDeLaPlantilla(t *testing.T) {
	views, out := newTestViews()
	plan := connectPlan()
	plan.Origin = application.TemplateUnsigned
	views.BootstrapPlan(plan)
	unsigned := out.String()
	assert.Contains(t, unsigned, "PLANTILLA SIN FIRMA DE VEX: LA REVISAS TÚ.")
	assert.Less(t, strings.Index(unsigned, "PLANTILLA SIN FIRMA"), strings.Index(unsigned, "Permisos que declara"), "antes de la lista que hay que revisar")
	assert.NotContains(t, unsigned, "catálogo firmado de vex para este commit")

	views, out = newTestViews()
	plan.Origin = application.TemplateSigned
	views.BootstrapPlan(plan)
	assert.Contains(t, out.String(), "es la que publica el catálogo firmado de vex para este commit")
	assert.NotContains(t, out.String(), "SIN FIRMA")
}

func TestViews_BootstrapPlan_ResaltaLoQueHayQueMirar(t *testing.T) {
	views, out := newTestViews()
	plan := connectPlan()
	plan.Mode, plan.IsOwner, plan.Resumed = application.BootstrapReconnect, false, true
	plan.Preview.Declared = append(plan.Preview.Declared, application.PlannedGrant{
		Identity: "deployPrincipalId", Role: "Owner", Scope: "toda la suscripción", Elevated: true, Reason: "da control sobre permisos",
	})

	views.BootstrapPlan(plan)

	text := out.String()
	assert.Contains(t, text, "Reconectar la nube de sand")
	assert.Contains(t, text, "Este proyecto pertenece a otra persona: lo que aceptes le da acceso a tu suscripción.")
	assert.Contains(t, text, "se retoma una conexión que ya estaba abierta")
	assert.Contains(t, text, "RECONECTAR CORTA EL ACCESO DE SAND HASTA QUE LA CONEXIÓN NUEVA SE VERIFIQUE")
	assert.Contains(t, text, "la anterior deja de obtener\n  tokens")
	assert.Contains(t, text, "`vex bootstrap sand`")
	assert.Contains(t, text, "‼ deployPrincipalId → Owner → toda la suscripción   [RESALTADO: da control sobre permisos]")
	assert.Contains(t, text, "‼ 1 permiso(s) resaltado(s)")
}

func TestViews_BootstrapPlan_NadaDeLaPlantillaFalsificaLaVista(t *testing.T) {
	views, out := newTestViews()
	plan := connectPlan()
	plan.ProjectName = "Pagos\x1b[2K\r‮IPA"
	plan.Preview.Changes = []application.PlannedChange{
		{Action: application.ChangeCreate, Kind: "tipo\x1b[1A", Name: "nombre\r\n  = sin cambios: 99 recurso(s)"},
	}

	views.BootstrapPlan(plan)

	text := out.String()
	assert.NotContains(t, text, "\x1b")
	assert.NotContains(t, text, "\r")
	assert.NotContains(t, text, "‮")
	assert.NotContains(t, text, "\n  = sin cambios: 99", "un nombre no puede abrir una línea propia")
}

func TestViews_BootstrapPlan_CuandoNoHayNadaQueAplicar(t *testing.T) {
	views, out := newTestViews()
	views.BootstrapPlan(&application.BootstrapPlan{Environment: "sand", Mode: application.BootstrapUpToDate})
	assert.Contains(t, out.String(), "sand ya está conectado con el bootstrap de este pipeline")
	assert.Contains(t, out.String(), "vex bootstrap sand --check")
	assert.Contains(t, out.String(), "vex bootstrap sand --reconnect")

	views, out = newTestViews()
	views.BootstrapPlan(&application.BootstrapPlan{Environment: "sand", Mode: application.BootstrapResume})
	assert.Contains(t, out.String(), "se retoma la verificación")

	views, out = newTestViews()
	views.BootstrapDone(application.BootstrapResult{Environment: "sand", Elapsed: 62 * time.Second})
	assert.Contains(t, out.String(), "Conexión de sand activa y verificada (1m02s).")
}

func TestViews_CheckReport(t *testing.T) {
	report := application.CheckReport{
		Environment: "sand", Contract: "azure/v1",
		Items: []application.CheckItem{
			{Status: application.CheckOK, Text: "La huella coincide"},
			{Status: application.CheckWarning, Text: "1 recurso sin vista previa"},
		},
	}
	views, out := newTestViews()
	views.CheckReport(report)
	assert.Contains(t, out.String(), "Auditoría de la conexión de sand (azure/v1)")
	assert.Contains(t, out.String(), okMark+" La huella coincide")
	assert.Contains(t, out.String(), warnMark+" 1 recurso sin vista previa")
	assert.Contains(t, out.String(), "Todo en orden. No se cambió nada.")

	report.Items = append(report.Items, application.CheckItem{Status: application.CheckFailed, Text: "Falta la credencial"})
	views, out = newTestViews()
	views.CheckReport(report)
	assert.Contains(t, out.String(), failMark+" Falta la credencial")
	assert.Contains(t, out.String(), "La conexión no está como vex la dejó. No se cambió nada.")
}

func TestViews_Disconnect(t *testing.T) {
	views, out := newTestViews()
	views.DisconnectWarning(&application.DisconnectPlan{Environment: "sand", Status: "active"})
	assert.Contains(t, out.String(), "Vas a desconectar la nube de sand (conexión active).")
	assert.Contains(t, out.String(), "el corte no es instantáneo")
	assert.Contains(t, out.String(), "No se borra ningún recurso ni permiso")

	views, out = newTestViews()
	views.Disconnected(application.DisconnectResult{Environment: "sand", TrustRemoved: true, Cleanup: []string{"az group delete --name rg"}})
	assert.Contains(t, out.String(), "sand desconectado: conexión revocada y confianza de vex retirada")
	assert.Contains(t, out.String(), "  az group delete --name rg\n")
	assert.Contains(t, out.String(), "hasta 10 el de vex")

	views, out = newTestViews()
	views.Disconnected(application.DisconnectResult{
		Environment: "sand", Pending: "un bloqueo lo impide", Manual: []string{"az lock delete --name l", "az identity federated-credential delete"},
	})
	assert.Contains(t, out.String(), "quedó revocado en el portal, pero la confianza de vex sigue en la nube: un bloqueo lo impide.")
	assert.Contains(t, out.String(), "Para retirarla tú:\n  az lock delete --name l\n  az identity federated-credential delete\n")
}
