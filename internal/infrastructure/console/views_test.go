package console

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
)

var viewsNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

const (
	idOK     = "01a113d0-74b0-7966-80ad-a84c35ab306c" // corto: 5ab306c
	idFailed = "01a113d0-7845-79ee-9381-be4f9efac5ab" // corto: efac5ab
	idDead   = "01a113d0-7999-7000-8000-000004f91b55" // corto: 4f91b55
	idOpen   = "01a113d0-7a00-7000-8000-0000000d3c1e" // corto: 00d3c1e
)

func newTestViews() (*Views, *bytes.Buffer) {
	var out bytes.Buffer
	return NewViews(&out).WithClock(func() time.Time { return viewsNow }), &out
}

func summary(id string, status application.AttemptStatus, cause application.AttemptCause, ago time.Duration) application.AttemptSummary {
	return application.AttemptSummary{
		ID: id, Environment: "sand", Requester: "jailux", UntilStep: "deploy",
		StartedAt: viewsNow.Add(-ago), Status: status, Cause: cause,
	}
}

func TestViews_AttemptList(t *testing.T) {
	views, out := newTestViews()

	views.AttemptList(application.AttemptList{Environment: "sand", Total: 4, Attempts: []application.AttemptSummary{
		summary(idOK, application.AttemptSucceeded, "", 5*time.Minute),
		summary(idFailed, application.AttemptFailed, application.CauseError, time.Hour),
		summary(idDead, application.AttemptFailed, application.CauseInterrupted, 30*time.Hour),
		summary(idOpen, "", "", 10*time.Second),
	}})

	assert.Equal(t, `sand · últimos 4 intentos
ID       HASTA   RESULTADO       QUIÉN   CUÁNDO
5ab306c  deploy  ✔ exitoso       jailux  hace 5 min
efac5ab  deploy  ✘ fallido       jailux  hace 1 h
4f91b55  deploy  ! interrumpido  jailux  ayer
00d3c1e  deploy  … en curso      jailux  hace unos segundos
→ vex show efac5ab · vex why efac5ab
`, out.String(), "la pista apunta al fallo más reciente que merece investigarse, no a un interrumpido")
}

func TestViews_AttemptList_SiNadaFalloLaPistaApuntaAlUltimo(t *testing.T) {
	views, out := newTestViews()

	views.AttemptList(application.AttemptList{Environment: "sand", Total: 1, Attempts: []application.AttemptSummary{
		summary(idOK, application.AttemptSucceeded, "", time.Hour),
	}})

	assert.Contains(t, out.String(), "→ vex show 5ab306c\n", "si nada falló, why no tendría nada que explicar")
	assert.NotContains(t, out.String(), "vex why")
	assert.Contains(t, out.String(), "sand · 1 intento\n")
}

func TestViews_AttemptList_MuestraCuantosHayEnTotal(t *testing.T) {
	views, out := newTestViews()

	views.AttemptList(application.AttemptList{Environment: "sand", Total: 37, Attempts: []application.AttemptSummary{
		summary(idOK, application.AttemptSucceeded, "", time.Hour),
	}})

	assert.Contains(t, out.String(), "sand · últimos 1 de 37 intentos\n")
}

func TestViews_AttemptList_Vacia(t *testing.T) {
	views, out := newTestViews()

	views.AttemptList(application.AttemptList{Environment: "sand"})

	assert.Equal(t, "sand: todavía no hay intentos.\n→ Ejecuta uno con: vex <paso> sand\n", out.String())
}

func TestViews_AttemptDetail(t *testing.T) {
	tests := []struct {
		name   string
		detail application.AttemptDetail
		want   string
	}{
		{
			name: "fallido en un paso",
			detail: application.AttemptDetail{
				AttemptSummary: summary(idFailed, application.AttemptFailed, "", time.Hour), Duration: 12 * time.Second,
				Steps: []application.StepOutcome{
					{Name: "test", Status: application.StepReused}, {Name: "package", Status: application.StepExecuted},
					{Name: "deploy", Status: application.StepFailed}, {Name: "cleanup", Status: application.StepPending},
				},
			},
			want: `Intento efac5ab · sand · hasta deploy
  jailux · hace 1 h · duró 12 s
✘ fallido en «deploy»
  ✔ test     reutilizado
  ✔ package  ejecutado
  ✘ deploy   falló
  · cleanup  no se llegó a él
→ vex log efac5ab --failed · vex why efac5ab
`,
		},
		{
			name: "interrumpido: un paso que empezó y nunca terminó",
			detail: application.AttemptDetail{
				AttemptSummary: summary(idDead, application.AttemptFailed, application.CauseInterrupted, 30*time.Hour),
				Steps:          []application.StepOutcome{{Name: "test", Status: application.StepUnfinished}},
			},
			want: `Intento 4f91b55 · sand · hasta deploy
  jailux · ayer
! interrumpido: su proceso murió sin terminar en «test»
  ! test  no terminó
→ vex log 4f91b55 · reintentar: vex deploy sand
`,
		},
		{
			name: "exitoso",
			detail: application.AttemptDetail{
				AttemptSummary: summary(idOK, application.AttemptSucceeded, "", 5*time.Minute), Duration: 800 * time.Millisecond,
				Steps: []application.StepOutcome{{Name: "deploy", Status: application.StepExecuted}},
			},
			want: `Intento 5ab306c · sand · hasta deploy
  jailux · hace 5 min · duró 800 ms
✔ exitoso
  ✔ deploy  ejecutado
→ vex log 5ab306c
`,
		},
		{
			name: "un error del motor, no de un comando",
			detail: application.AttemptDetail{
				AttemptSummary: summary(idFailed, application.AttemptFailed, application.CauseError, time.Minute),
				Steps:          []application.StepOutcome{{Name: "test", Status: application.StepUnfinished}},
			},
			want: `Intento efac5ab · sand · hasta deploy
  jailux · hace 1 min
✘ se detuvo por un error al ejecutar (no por un comando) en «test»
  ! test  no terminó
→ vex log efac5ab --failed · vex why efac5ab
`,
		},
		{
			name: "en curso no da pistas ni duración",
			detail: application.AttemptDetail{
				AttemptSummary: summary(idOpen, "", "", 5*time.Second), Duration: 3 * time.Second,
				Steps: []application.StepOutcome{{Name: "test", Status: application.StepPending}},
			},
			want: `Intento 00d3c1e · sand · hasta deploy
  jailux · hace unos segundos
… en curso (si no avanza, su proceso pudo morir: el motor lo recupera solo al lanzar otro intento)
  · test  no se llegó a él
`,
		},
		{
			name: "un rollback dice a dónde volvió",
			detail: application.AttemptDetail{
				AttemptSummary: summary(idOK, application.AttemptSucceeded, "", time.Hour), RolledBackTo: idFailed,
			},
			want: `Intento 5ab306c · sand · hasta deploy
  jailux · hace 1 h
✔ exitoso
  ↩ volvió al despliegue efac5ab
→ vex log 5ab306c
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			views, out := newTestViews()

			views.AttemptDetail(tt.detail)

			assert.Equal(t, tt.want, out.String())
		})
	}
}

func TestViews_Logs(t *testing.T) {
	t.Run("los comandos fallidos", func(t *testing.T) {
		views, out := newTestViews()

		views.Logs(application.AttemptLogs{AttemptID: idFailed, Outputs: []application.CommandOutput{
			{Step: "deploy", Command: "terraform apply", Text: "Error: no valid credential sources\nexit 1\n"},
		}}, true)

		assert.Equal(t, `Intento efac5ab · salida de los comandos fallidos
✘ deploy › terraform apply
  Error: no valid credential sources
  exit 1
`, out.String())
	})

	t.Run("todo, con los exitosos marcados", func(t *testing.T) {
		views, out := newTestViews()

		views.Logs(application.AttemptLogs{AttemptID: idOK, Outputs: []application.CommandOutput{
			{Step: "test", Command: "mvn test", Succeeded: true, Text: "BUILD SUCCESS\n"},
			{Step: "deploy", Command: "echo hola", Succeeded: true},
		}}, false)

		assert.Equal(t, `Intento 5ab306c · salida de los comandos
✔ test › mvn test
  BUILD SUCCESS
✔ deploy › echo hola
`, out.String())
	})

	t.Run("sin id dice que es el último", func(t *testing.T) {
		views, out := newTestViews()

		views.Logs(application.AttemptLogs{}, false)

		assert.Contains(t, out.String(), "Último intento · salida de los comandos\n")
	})

	t.Run("ningún comando falló", func(t *testing.T) {
		views, out := newTestViews()

		views.Logs(application.AttemptLogs{AttemptID: idOK}, true)

		assert.Equal(t, `Intento 5ab306c · salida de los comandos fallidos
Ningún comando falló.
→ vex log (sin --failed) para ver toda la salida
`, out.String())
	})

	t.Run("sin salida porque todo se reutilizó", func(t *testing.T) {
		views, out := newTestViews()

		views.Logs(application.AttemptLogs{AttemptID: idOK}, false)

		assert.Contains(t, out.String(), "No hay salida guardada: los pasos reutilizados no ejecutan comandos.\n")
	})
}

func TestViews_DeploymentList(t *testing.T) {
	views, out := newTestViews()

	views.DeploymentList(application.DeploymentList{Environment: "prod", Total: 3, Items: []application.DeploymentView{
		{
			Deployment: application.Deployment{ID: idOK, AttemptID: idOpen, At: viewsNow.Add(-48 * time.Hour)},
			Release:    &application.Release{Name: "v2.1", Version: 5}, Current: true,
		},
		{
			Deployment: application.Deployment{ID: idFailed, AttemptID: idDead, At: viewsNow.Add(-5 * 24 * time.Hour)},
			Release:    &application.Release{Name: "2", Version: 2},
		},
		{Deployment: application.Deployment{ID: idDead, AttemptID: idOK, At: viewsNow.Add(-9 * 24 * time.Hour)}},
	}})

	assert.Equal(t, `prod · últimos 3 despliegues
ID       CUÁNDO    INTENTO  LANZAMIENTO
5ab306c  hace 2 d  00d3c1e  ● v2.1 (actual)
efac5ab  hace 5 d  4f91b55  v2
4f91b55  hace 9 d  5ab306c  —
→ vex rollback <id> · vex release prod <id>
`, out.String())
}

func TestViews_DeploymentList_Vacia(t *testing.T) {
	views, out := newTestViews()

	views.DeploymentList(application.DeploymentList{Environment: "prod"})

	assert.Equal(t, `prod: todavía no hay despliegues.
  Un despliegue nace cuando un intento ejecuta TODOS los pasos del pipeline con éxito.
→ vex <último paso> prod
`, out.String())
}

func TestViews_ReleaseList(t *testing.T) {
	views, out := newTestViews()

	views.ReleaseList(application.ReleaseList{Environment: "prod", Total: 2, Releases: []application.Release{
		{ID: "l2", DeploymentID: idOK, Version: 5, Name: "v2.1", At: viewsNow.Add(-time.Hour)},
		{ID: "l1", DeploymentID: idFailed, Version: 2, Name: "2", At: viewsNow.Add(-48 * time.Hour)},
	}})

	assert.Equal(t, `prod · últimos 2 lanzamientos
LANZAMIENTO      VERSIÓN  DESPLIEGUE  CUÁNDO
● v2.1 (actual)  5        5ab306c     hace 1 h
v2               2        efac5ab     hace 2 d
→ vex deployments prod
`, out.String())
}

func TestViews_ReleaseList_Vacia(t *testing.T) {
	views, out := newTestViews()

	views.ReleaseList(application.ReleaseList{Environment: "prod"})

	assert.Equal(t, "prod: todavía no se ha lanzado nada.\n→ vex release prod <despliegue> · vex deployments prod\n", out.String())
}

func TestListTitle(t *testing.T) {
	assert.Equal(t, "1 intento", listTitle(1, 1, "intento", "intentos"))
	assert.Equal(t, "últimos 3 intentos", listTitle(3, 3, "intento", "intentos"))
	assert.Equal(t, "últimos 10 de 37 intentos", listTitle(10, 37, "intento", "intentos"))
}

func TestViews_Environments(t *testing.T) {
	views, out := newTestViews()

	views.Environments([]application.Environment{
		{Value: "sand", Name: "sandbox", Description: "desarrollo y pruebas"},
		{Value: "stag", Name: "staging", Description: "validación previa"},
		{Value: "prod", Name: "production", Description: "usuarios reales", Protected: true},
	})

	assert.Equal(t, `Ambientes del pipeline, en su orden:
  VALOR  NOMBRE      LANZAMIENTO         DESCRIPCIÓN
  sand   sandbox     automático          desarrollo y pruebas
  stag   staging     automático          validación previa
  prod   production  manual (protegido)  usuarios reales
→ en los protegidos el lanzamiento lo decides tú: vex release <ambiente> <despliegue>
`, out.String())
}

func TestViews_Environments_SinProtegidosSugiereEjecutar(t *testing.T) {
	views, out := newTestViews()

	views.Environments([]application.Environment{{Value: "sand", Name: "sandbox"}})

	assert.Contains(t, out.String(), "→ vex <step> <environment> ejecuta en uno de ellos\n")
	assert.NotContains(t, out.String(), "protegido")
}

func TestViews_Environments_Vacio(t *testing.T) {
	views, out := newTestViews()

	views.Environments(nil)

	assert.Equal(t, "El pipeline no declara ningún ambiente.\n", out.String())
}

func TestViews_Steps(t *testing.T) {
	views, out := newTestViews()

	views.Steps([]application.PipelineStep{
		{Name: "test", Order: 1}, {Name: "acr", Order: 2, Shared: true}, {Name: "deploy", Order: 3},
	}, nil)

	assert.Equal(t, `Pasos del pipeline, en orden:
  1  test
  2  acr     compartido entre ambientes
  3  deploy
→ vex <step> <environment> ejecuta hasta ese paso (y los anteriores)
`, out.String())
}

func TestViews_Steps_AvisaDeLosQueChocanConUnComando(t *testing.T) {
	views, out := newTestViews()

	views.Steps([]application.PipelineStep{{Name: "test", Order: 1}, {Name: "release", Order: 2}},
		map[string]bool{"release": true})

	assert.Contains(t, out.String(),
		"! El paso «release» se llama igual que un comando de vex: ejecútalo con `vex run release <ambiente>`.")
	assert.NotContains(t, out.String(), "«test» se llama")
}

func TestViews_CheckPassed(t *testing.T) {
	views, out := newTestViews()

	views.CheckPassed("sand", "deploy")

	assert.Equal(t, "✔ Todo en orden: el pipeline es válido y las variables de sand hasta «deploy» se resuelven.\n  No se ejecutó nada.\n", out.String())
}

func TestViews_Why_ConDiagnostico(t *testing.T) {
	views, out := newTestViews()

	views.Why(application.WhyReport{
		AttemptID: idFailed, Environment: "sand", Outcome: application.WhyDiagnosed,
		Diagnosis: application.Diagnosis{
			Kind:          application.DiagnosisFound,
			Reference:     application.DiagnosedAttempt{ID: idOK, At: viewsNow.Add(-48 * time.Hour)},
			AttemptsSince: 3,
			Changes: application.DiagnosisChanges{
				Code:         []string{"package"},
				Instructions: []string{"deploy", "supply"},
				DeclaredVars: []application.VariableRef{{Step: "deploy", Name: "db_url"}},
				ProducedVars: []application.VariableRef{{Step: "test", Name: "etiqueta"}, {Step: "package", Name: "imagen"}},
			},
		},
	})

	assert.Equal(t, `¿Por qué falló efac5ab en sand?
Se compara con la última vez que funcionó: el intento 5ab306c (hace 2 d). Desde entonces se han hecho 3 intentos, este incluido.
Desde entonces cambió:
  • el código              → paso package
  • las instrucciones      → pasos deploy, supply
  • variables declaradas   → deploy.db_url
  • variables producidas   → test.etiqueta, package.imagen
→ vex log efac5ab --failed · vex show efac5ab
`, out.String())
}

func TestViews_Why_PrimerIntentoDesdeLaReferencia(t *testing.T) {
	views, out := newTestViews()

	views.Why(application.WhyReport{
		AttemptID: idFailed, Environment: "sand", Outcome: application.WhyDiagnosed,
		Diagnosis: application.Diagnosis{
			Kind: application.DiagnosisFound, Reference: application.DiagnosedAttempt{ID: idOK, At: viewsNow.Add(-time.Hour)},
			AttemptsSince: 1, Changes: application.DiagnosisChanges{Code: []string{"test"}},
		},
	})

	assert.Contains(t, out.String(), "(hace 1 h). Este es el primer intento desde entonces.\n")
}

func TestViews_Why_NadaCambio(t *testing.T) {
	views, out := newTestViews()

	views.Why(application.WhyReport{
		AttemptID: idFailed, Environment: "sand", Outcome: application.WhyDiagnosed,
		Diagnosis: application.Diagnosis{Kind: application.DiagnosisFound, Reference: application.DiagnosedAttempt{ID: idOK, At: viewsNow.Add(-time.Hour)}},
	})

	assert.Equal(t, `¿Por qué falló efac5ab en sand?
Se compara con la última vez que funcionó: el intento 5ab306c (hace 1 h).
No cambió nada de lo que mira cada paso: el fallo no viene de un cambio en el pipeline.
→ revisa lo externo (credenciales, red, servicios) con: vex log efac5ab --failed
`, out.String())
}

func TestViews_Why_SinNadaQueDiagnosticar(t *testing.T) {
	tests := []struct {
		outcome application.WhyOutcome
		want    string
	}{
		{application.WhyNotAFailure, "El intento efac5ab terminó bien: no hay nada que diagnosticar.\n→ vex show efac5ab\n"},
		{application.WhyCanceled, "El intento efac5ab se canceló: una cancelación no es un fallo, no hay nada que diagnosticar.\n→ vex show efac5ab\n"},
		{application.WhyInterrupted, "El intento efac5ab se interrumpió (su proceso murió): no falló por un cambio, no hay nada que diagnosticar.\n→ vex show efac5ab\n"},
		{application.WhyInProgress, "El intento efac5ab no ha terminado: todavía no hay un fallo que diagnosticar.\n→ vex show efac5ab\n"},
		{application.WhyNotAttributable, "El motor no atribuye una causa al intento efac5ab (se canceló, se interrumpió o se abandonó).\n→ vex show efac5ab\n"},
	}
	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			views, out := newTestViews()

			views.Why(application.WhyReport{AttemptID: idFailed, Outcome: tt.outcome})

			assert.Equal(t, tt.want, out.String())
		})
	}
}

func TestViews_Why_SinReferencia(t *testing.T) {
	views, out := newTestViews()

	views.Why(application.WhyReport{AttemptID: idFailed, Environment: "sand", Outcome: application.WhyNoReference})

	assert.Equal(t, `¿Por qué falló efac5ab en sand?
No hay ninguna ejecución anterior que funcionara en sand con la que comparar.
  Hace falta un despliegue exitoso previo para saber qué cambió.
→ vex log efac5ab --failed para ver el error
`, out.String())
}

func TestViews_Why_SinReferenciaYSinAmbienteConocido(t *testing.T) {
	views, out := newTestViews()

	views.Why(application.WhyReport{AttemptID: idFailed, Outcome: application.WhyNoReference})

	assert.Contains(t, out.String(), "¿Por qué falló efac5ab?\n")
	assert.Contains(t, out.String(), "No hay ninguna ejecución anterior que funcionara con la que comparar.")
}

func TestViews_Abandono(t *testing.T) {
	views, out := newTestViews()
	plan := application.AbandonPlan{Attempt: application.AttemptDetail{
		AttemptSummary: summary(idDead, "", "", 2*time.Hour),
	}}

	views.AbandonWarning(plan)
	views.Abandoned(plan)

	assert.Equal(t, `Intento 4f91b55 · sand · hace 2 h · sin terminar
Abandonarlo libera el ambiente sand para nuevas ejecuciones, pero NO detiene comandos que su proceso siga ejecutando.
✔ Intento 4f91b55 abandonado: el ambiente sand queda libre.
→ vex <paso> sand
`, out.String())
}

func TestViews_AttemptList_UnAbandonadoNoSeMuestraComoEnCurso(t *testing.T) {
	views, out := newTestViews()
	abandoned := summary(idDead, "", "", 3*time.Hour)
	abandoned.Abandoned = true

	views.AttemptList(application.AttemptList{Environment: "sand", Total: 1, Attempts: []application.AttemptSummary{abandoned}})

	assert.Contains(t, out.String(), "– abandonado")
	assert.NotContains(t, out.String(), "en curso")
}

func TestViews_RollbackWarning(t *testing.T) {
	t.Run("con ambiente y fecha conocidos", func(t *testing.T) {
		views, out := newTestViews()

		views.RollbackWarning(application.RollbackPlan{DeploymentID: idFailed, Environment: "prod", DeployedAt: viewsNow.Add(-48 * time.Hour)})

		assert.Equal(t, "Volverás a desplegar en prod los mismos commits del despliegue efac5ab (hace 2 d).\nSe ejecutarán TODOS los pasos del pipeline.\n", out.String())
	})
	t.Run("de un despliegue que el CLI no ha visto", func(t *testing.T) {
		views, out := newTestViews()

		views.RollbackWarning(application.RollbackPlan{DeploymentID: idFailed})

		assert.Equal(t, "Volverás a desplegar los mismos commits del despliegue efac5ab, en el ambiente en que se hizo.\nSe ejecutarán TODOS los pasos del pipeline.\n", out.String())
	})
}

func TestViews_ReleaseProtectUnprotect(t *testing.T) {
	views, out := newTestViews()

	views.Released(application.Release{Environment: "prod", DeploymentID: idFailed, Version: 5, Name: "v2.1"})
	views.Released(application.Release{Environment: "prod", DeploymentID: idFailed, Version: 6})
	views.Protected("prod")
	views.Unprotected("prod")

	assert.Equal(t, "✔ prod: lanzado el despliegue efac5ab como «v2.1» (versión 5).\n→ vex deployments prod\n"+
		"✔ prod: lanzado el despliegue efac5ab como versión 6.\n→ vex deployments prod\n"+
		"✔ prod protegido: sus despliegues ya no se lanzan solos.\n  Ojo: esto NO impide desplegar ni hacer rollback; solo el lanzamiento automático.\n→ vex release prod <despliegue>\n"+
		"✔ prod ya no está protegido: cada despliegue exitoso se lanza solo.\n→ vex envs\n", out.String())
}
