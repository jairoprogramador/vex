package application

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

const (
	testProjectID    = "b5a1c2d4-0000-4000-8000-0000000000b1"
	testConnectionID = "c0ffee00-0000-4000-8000-0000000000c1"
	testIssuer       = "https://id.vex.test"
	testAudience     = "aud://nube"
	testSubject      = "vex:p:" + testProjectID + ":e:sand:c:" + testConnectionID
)

// fakeBootstrapProvider es una nube inventada que anota lo que se le pide, en orden.
type fakeBootstrapProvider struct {
	log         *[]string
	account     CloudAccount
	accountErr  error
	validateErr error
	registerErr error
	preview     BootstrapPreview
	previewErr  error
	outputs     ConnectionOutputs
	applyErr    error
	requests    []BootstrapRequest
}

func (f *fakeBootstrapProvider) Account(context.Context) (CloudAccount, error) {
	*f.log = append(*f.log, "account")
	return f.account, f.accountErr
}
func (f *fakeBootstrapProvider) Region(requested string, current *ConnectionOutputs) (string, error) {
	if requested == "" && current != nil {
		requested = current.Params["region"]
	}
	if requested == "" {
		return "", errors.New("indica la región")
	}
	return requested, nil
}
func (f *fakeBootstrapProvider) ValidateTemplate(PipelineBootstrap) error {
	*f.log = append(*f.log, "validate")
	return f.validateErr
}
func (f *fakeBootstrapProvider) RegisterProviders(_ context.Context, _ CloudAccount, namespaces []string) error {
	*f.log = append(*f.log, "register:"+strings.Join(namespaces, ","))
	return f.registerErr
}
func (f *fakeBootstrapProvider) Preview(_ context.Context, req BootstrapRequest) (BootstrapPreview, error) {
	*f.log = append(*f.log, "preview")
	f.requests = append(f.requests, req)
	return f.preview, f.previewErr
}
func (f *fakeBootstrapProvider) Apply(_ context.Context, req BootstrapRequest, seal string) (ConnectionOutputs, error) {
	*f.log = append(*f.log, "apply:"+seal)
	f.requests = append(f.requests, req)
	return f.outputs, f.applyErr
}

type fakeTrust struct {
	log       *[]string
	ensured   []Trust
	ensureErr error
	findings  []TrustFinding
	removeErr error
	removed   []Trust
	// exposed son los permisos sin declarar que Exposure devuelve; exposureErr, su error.
	exposed     []PlannedGrant
	exposureErr error
	previews    []BootstrapPreview
}

func (f *fakeTrust) Audience() string { return testAudience }
func (f *fakeTrust) Ensure(_ context.Context, _ CloudAccount, _ ConnectionOutputs, want Trust) error {
	*f.log = append(*f.log, "trust.ensure")
	f.ensured = append(f.ensured, want)
	return f.ensureErr
}
func (f *fakeTrust) Exposure(_ context.Context, _ CloudAccount, preview BootstrapPreview, _ ConnectionOutputs, _ Trust) ([]PlannedGrant, error) {
	*f.log = append(*f.log, "trust.exposure")
	f.previews = append(f.previews, preview)
	return f.exposed, f.exposureErr
}
func (f *fakeTrust) Inspect(_ context.Context, _ CloudAccount, _ ConnectionOutputs, want Trust) ([]TrustFinding, error) {
	*f.log = append(*f.log, "trust.inspect:"+want.Subject)
	return f.findings, nil
}
func (f *fakeTrust) Remove(_ context.Context, _ CloudAccount, _ ConnectionOutputs, want Trust) error {
	*f.log = append(*f.log, "trust.remove")
	f.removed = append(f.removed, want)
	return f.removeErr
}
func (f *fakeTrust) Cleanup(ConnectionOutputs, string) CleanupGuide {
	return CleanupGuide{RemoveTrust: []string{"nube confianza borrar"}, DeleteResources: []string{"nube grupo borrar"}}
}

// answer es una respuesta del portal a complete o verify.
type answer struct {
	v   Verification
	err error
}

type fakeAdmin struct {
	log         *[]string
	created     CreatedConnection
	createErr   error
	drafts      []ConnectionDraft
	completions []ConnectionCompletion
	answers     []answer // por llamada a complete o verify; la última se repite
	asked       int
	revoked     []string
	revokeErr   error
}

func (f *fakeAdmin) Create(_ context.Context, draft ConnectionDraft) (CreatedConnection, error) {
	*f.log = append(*f.log, "create")
	f.drafts = append(f.drafts, draft)
	return f.created, f.createErr
}
func (f *fakeAdmin) next() (Verification, error) {
	if len(f.answers) == 0 {
		return Verification{Status: VerificationActive}, nil
	}
	a := f.answers[min(f.asked, len(f.answers)-1)]
	f.asked++
	return a.v, a.err
}
func (f *fakeAdmin) Complete(_ context.Context, completion ConnectionCompletion) (Verification, error) {
	*f.log = append(*f.log, "complete")
	f.completions = append(f.completions, completion)
	return f.next()
}
func (f *fakeAdmin) Verify(_ context.Context, connectionID string) (Verification, error) {
	*f.log = append(*f.log, "verify:"+connectionID)
	return f.next()
}
func (f *fakeAdmin) Revoke(_ context.Context, connectionID string) error {
	*f.log = append(*f.log, "revoke")
	f.revoked = append(f.revoked, connectionID)
	return f.revokeErr
}

type fakeJournal struct {
	records []AcceptanceRecord
	err     error
}

func (f *fakeJournal) Record(record AcceptanceRecord) error {
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, record)
	return nil
}

func (f *fakeJournal) outcomes() []string {
	out := make([]string, len(f.records))
	for i, record := range f.records {
		out[i] = record.Action + ":" + record.Outcome
	}
	return out
}

// bootstrapRig arma los servicios con una nube y un portal falsos. Por defecto el ambiente no tiene conexión.
type bootstrapRig struct {
	catalog    *fakeCatalog
	log        []string
	provider   *fakeBootstrapProvider
	trust      *fakeTrust
	admin      *fakeAdmin
	journal    *fakeJournal
	conns      *fakeCloudConnections
	bootstraps *fakeBootstraps
	login      *fakeLogin
	notify     *fakePresenter
	service    *BootstrapService
	disconnect *DisconnectService
	now        time.Time
	slept      []time.Duration
}

func cloudTarget() map[string]string { return map[string]string{"cuenta": "123"} }

func connectionOutputs() ConnectionOutputs {
	return ConnectionOutputs{ContractID: "nube/v1", Params: map[string]string{"cuenta": "123", "region": "sur", "identidad": "id-1"}}
}

func newBootstrapRig(t *testing.T, opts ...projectOption) *bootstrapRig {
	t.Helper()
	project := newProject(t, append([]projectOption{withCloud(t, "", proVos.CredentialsPortal)}, opts...)...)
	id, err := proVos.NewProjectID(testProjectID)
	require.NoError(t, err)
	project.WithID(id)

	r := &bootstrapRig{now: fixedNow()}
	r.provider = &fakeBootstrapProvider{
		log:     &r.log,
		account: CloudAccount{Summary: []LabeledValue{{Label: "Cuenta", Value: "ana"}}, Target: cloudTarget()},
		preview: BootstrapPreview{Seal: "sello-1", Changes: []PlannedChange{{Action: ChangeCreate, Kind: "grupo", Name: "rg"}}},
		outputs: connectionOutputs(),
	}
	r.trust = &fakeTrust{log: &r.log}
	r.admin = &fakeAdmin{log: &r.log, created: CreatedConnection{
		ConnectionID: testConnectionID, Subject: testSubject, Issuer: testIssuer, Audience: testAudience,
		Project: ConnectedProject{ID: testProjectID, Name: "Pagos API", Slug: "pagos-api-b5a1c2"},
		Owner:   ProjectOwner{Name: "Ana", Email: "ana@ejemplo.com"}, IsOwner: true,
	}}
	r.journal = &fakeJournal{}
	r.conns = &fakeCloudConnections{info: CloudConnectionInfo{Status: "absent", Hint: "vex bootstrap sand"}}
	r.bootstraps = &fakeBootstraps{bootstrap: PipelineBootstrap{
		Cloud: "nube", Dir: "/cache/pipe.git/bootstrap/nube", Fingerprint: testFingerprint,
		Manifest: BootstrapManifest{Contract: "nube/v1", Template: "main.json", Providers: []string{"Nube.Red"}, CloudSteps: []string{"deploy"}},
	}}
	r.login, r.notify = &fakeLogin{}, &fakePresenter{}

	adapters := NewCloudAdapters(fakeCloudAdapter{contract: "nube/v1", bootstrap: r.provider, trust: r.trust})
	repo := &stubProjectRepo{loaded: project, existsBool: true}
	base := t.TempDir()
	workspace := NewWorkspace(repo, &fakeSources{}, &fakeImages{}, r.notify, LocalExecutorConfig{
		WorkDir: "/work", StoreDir: filepath.Join(base, "store"), SpaceDir: filepath.Join(base, "space"), MaterialDir: filepath.Join(base, "material"),
	})
	r.catalog = signedCatalog()
	preparer := NewCloudPreparer(r.login, &fakeProjectPortal{}, repo, r.conns, r.bootstraps, adapters, nil, r.notify).
		WithTemplateCatalog(r.catalog)
	r.service = NewBootstrapService(workspace, preparer, r.bootstraps, adapters, r.admin, r.journal, r.notify, testIssuer)
	r.service.now = func() time.Time { return r.now }
	r.service.sleep = func(ctx context.Context, d time.Duration) error {
		r.slept = append(r.slept, d)
		r.now = r.now.Add(d)
		return ctx.Err()
	}
	r.disconnect = NewDisconnectService(workspace, preparer, adapters, r.admin, r.journal, r.notify, testIssuer)
	return r
}

func (r *bootstrapRig) activeConnection() {
	outputs := connectionOutputs()
	r.conns.info = CloudConnectionInfo{
		Status: ConnectionActive, ConnectionID: testConnectionID, BootstrapSHA256: testFingerprint, Outputs: &outputs,
	}
}

func (r *bootstrapRig) plan(t *testing.T, opts BootstrapOptions) *BootstrapPlan {
	t.Helper()
	if opts.Environment == "" {
		opts.Environment = "sand"
	}
	plan, err := r.service.Plan(context.Background(), opts)
	require.NoError(t, err)
	return plan
}

var yes = Acceptance{Confirmed: true}

func TestBootstrap_PrimeraConexion(t *testing.T) {
	r := newBootstrapRig(t)

	plan := r.plan(t, BootstrapOptions{Region: "sur"})

	assert.Equal(t, BootstrapConnect, plan.Mode)
	assert.Equal(t, []string{"validate", "account", "create", "register:Nube.Red", "preview"}, r.log,
		"reglas estáticas y sesión antes de abrir nada; nada se aplica al planear")
	require.Len(t, r.admin.drafts, 1)
	assert.Equal(t, ConnectionDraft{
		ProjectID: testProjectID, Environment: "sand", Contract: "nube/v1", BootstrapSHA256: testFingerprint,
		PipelineCommit: "commit-of-pipe.git", Target: cloudTarget(),
	}, r.admin.drafts[0])
	assert.Equal(t, "Pagos API", plan.ProjectName)
	assert.Equal(t, "commit-of-pipe.git", plan.PipelineCommit)
	assert.Equal(t, "https://github.com/local/pipe.git", plan.PipelineURL)
	assert.Equal(t, BootstrapRequest{
		Account: r.provider.account, Bootstrap: r.bootstraps.bootstrap, Name: "vex-bootstrap-pagos-api-b5a1c2-sand",
		ProjectID: testProjectID, ProjectSlug: "pagos-api-b5a1c2", Environment: "sand", Region: "sur",
	}, r.provider.requests[0])
	assert.Empty(t, plan.TypedConfirmation(), "sin permisos resaltados basta un sí")

	r.log = nil
	result, err := r.service.Apply(context.Background(), plan, yes)

	require.NoError(t, err)
	assert.Equal(t, testConnectionID, result.ConnectionID)
	assert.Equal(t, []string{"apply:sello-1", "trust.exposure", "trust.ensure", "complete"}, r.log,
		"la identidad se revisa después de aplicar y antes de confiar")
	assert.Equal(t, []Trust{{Issuer: testIssuer, Subject: testSubject, Audience: testAudience}}, r.trust.ensured,
		"el emisor es el del binario y el subject, el recalculado")
	assert.Equal(t, ConnectionCompletion{
		ConnectionID: testConnectionID, Target: cloudTarget(), BootstrapSHA256: testFingerprint, Outputs: connectionOutputs(),
	}, r.admin.completions[0])
	assert.Equal(t, []string{"bootstrap:accepted", "bootstrap:active"}, r.journal.outcomes())
	assert.Equal(t, "commit-of-pipe.git", r.journal.records[0].PipelineCommit)
	assert.Equal(t, testFingerprint, r.journal.records[0].Fingerprint)
}

func TestBootstrap_NoSeFiaDeLoQueDevuelveElPortal(t *testing.T) {
	tests := map[string]struct {
		change func(*CreatedConnection)
		want   string
	}{
		"otro emisor":    {func(c *CreatedConnection) { c.Issuer = "https://id.atacante.test" }, "emisor de tokens distinto"},
		"otra audiencia": {func(c *CreatedConnection) { c.Audience = "aud://otra" }, "audiencia distinta"},
		"otro subject": {func(c *CreatedConnection) { c.Subject = strings.Replace(testSubject, ":e:sand:", ":e:prod:", 1) },
			"subject que no corresponde"},
		"otro proyecto":    {func(c *CreatedConnection) { c.Project.ID = "ffffffff-0000-4000-8000-0000000000b1" }, "proyecto que no corresponde"},
		"otro slug":        {func(c *CreatedConnection) { c.Project.Slug = "otro-proyecto-b5a1c2" }, "proyecto que no corresponde"},
		"id de otra forma": {func(c *CreatedConnection) { c.ConnectionID = "../../x" }, "no se puede usar"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newBootstrapRig(t)
			tt.change(&r.admin.created)

			_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "sur"})

			assert.ErrorContains(t, err, tt.want)
			assert.Equal(t, []string{"validate", "account", "create"}, r.log, "no se registra ni se previsualiza nada")
		})
	}
}

func TestBootstrap_LoQueImpideEmpezar(t *testing.T) {
	t.Run("la plantilla rompe una regla estática: no se abre nada", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.provider.validateErr = &BootstrapRejectedError{Rule: "S1", Detail: "crea una credencial federada"}

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "sur"})

		var rejected *BootstrapRejectedError
		require.ErrorAs(t, err, &rejected)
		assert.Equal(t, []string{"validate"}, r.log)
		assert.Empty(t, r.conns.calls)
	})
	t.Run("sin sesión de la nube", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.provider.accountErr = errors.New("ejecuta `nube login`")

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "sur"})

		assert.ErrorContains(t, err, "nube login")
		assert.NotContains(t, r.log, "create")
	})
	t.Run("sin región la primera vez", func(t *testing.T) {
		r := newBootstrapRig(t)

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand"})

		assert.ErrorContains(t, err, "indica la región")
		assert.NotContains(t, r.log, "create")
	})
	t.Run("el proyecto no usa el portal", func(t *testing.T) {
		r := newBootstrapRig(t, withCloud(t, "", proVos.CredentialsEnv))

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand"})

		require.ErrorIs(t, err, ErrBootstrapNeedsPortal)
		assert.Zero(t, r.login.ensures)
	})
	t.Run("binario sin emisor", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.service.issuer = ""

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand"})

		require.ErrorIs(t, err, ErrIssuerNotConfigured)
		assert.Empty(t, r.log)
	})
	t.Run("ambiente con forma rara", func(t *testing.T) {
		r := newBootstrapRig(t)

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand; rm -rf"})

		assert.ErrorContains(t, err, "no es un nombre de ambiente válido")
		assert.Zero(t, r.login.ensures)
	})
	t.Run("el portal rechaza abrir la conexión", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.admin.createErr = &ConnectionRejectedError{Code: "not_project_admin"}

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "sur"})

		assert.ErrorContains(t, err, "solo quien administra el proyecto")
		assert.NotContains(t, r.log, "preview")
	})
	t.Run("la vista previa rompe una regla", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.provider.previewErr = &BootstrapRejectedError{Rule: "V1", Detail: "borraría rg"}

		_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "sur"})

		assert.ErrorContains(t, err, "regla V1")
	})
}

func TestBootstrap_SegunLaConexionQueYaHay(t *testing.T) {
	t.Run("activa y con el mismo bootstrap: nada que hacer", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.activeConnection()

		plan := r.plan(t, BootstrapOptions{})

		assert.Equal(t, BootstrapUpToDate, plan.Mode)
		assert.False(t, plan.NeedsAcceptance())
		assert.Equal(t, []string{"validate", "account"}, r.log)
		_, err := r.service.Apply(context.Background(), plan, Acceptance{})
		require.NoError(t, err)
		assert.Equal(t, []string{"validate", "account"}, r.log)
	})
	t.Run("activa y el bootstrap cambió: reconecta, con la región de la conexión", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.activeConnection()
		r.conns.info.BootstrapSHA256 = strings.Repeat("0", 64)

		plan := r.plan(t, BootstrapOptions{})

		assert.Equal(t, BootstrapReconnect, plan.Mode)
		assert.True(t, r.admin.drafts[0].Reconnect)
		assert.Equal(t, "sur", plan.Region)
	})
	t.Run("activa y se pide --reconnect", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.activeConnection()

		plan := r.plan(t, BootstrapOptions{Reconnect: true})

		assert.Equal(t, BootstrapReconnect, plan.Mode)
		assert.True(t, r.admin.drafts[0].Reconnect)
	})
	t.Run("activa en otro destino que la sesión: reconecta", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.activeConnection()
		r.provider.account.Target = map[string]string{"cuenta": "999"}

		plan := r.plan(t, BootstrapOptions{})

		assert.Equal(t, BootstrapReconnect, plan.Mode)
	})
	t.Run("verificándose con el mismo bootstrap: solo verifica", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.conns.info = CloudConnectionInfo{Status: VerificationVerifying, ConnectionID: testConnectionID, BootstrapSHA256: testFingerprint}

		plan := r.plan(t, BootstrapOptions{})
		_, err := r.service.Apply(context.Background(), plan, Acceptance{})

		require.NoError(t, err)
		assert.Equal(t, BootstrapResume, plan.Mode)
		assert.Equal(t, []string{"validate", "account", "verify:" + testConnectionID}, r.log)
	})
	t.Run("fallida o revocada: conexión nueva sin reconnect", func(t *testing.T) {
		for _, status := range []string{"failed", "revoked", "pending", "absent"} {
			r := newBootstrapRig(t)
			r.conns.info = CloudConnectionInfo{Status: status, ConnectionID: testConnectionID, BootstrapSHA256: testFingerprint}

			plan := r.plan(t, BootstrapOptions{Region: "sur"})

			assert.Equal(t, BootstrapConnect, plan.Mode, status)
			assert.False(t, r.admin.drafts[0].Reconnect, status)
		}
	})
	t.Run("el portal retoma una conexión a medias", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.admin.created.Resumed = true

		plan := r.plan(t, BootstrapOptions{Region: "sur"})

		assert.True(t, plan.Resumed)
	})
}

func elevatedPreview() BootstrapPreview {
	return BootstrapPreview{
		Seal:     "sello-1",
		Declared: []PlannedGrant{{Identity: "deploy", Role: "Dueño", Scope: "toda la cuenta", Elevated: true, Reason: "controla permisos"}},
	}
}

func TestBootstrap_SinAceptacionNoSeAplica(t *testing.T) {
	tests := map[string]struct {
		preview    BootstrapPreview
		acceptance Acceptance
		accepted   bool
	}{
		"sin responder":                         {BootstrapPreview{Seal: "sello-1"}, Acceptance{}, false},
		"sí":                                    {BootstrapPreview{Seal: "sello-1"}, yes, true},
		"permisos resaltados con un sí a secas": {elevatedPreview(), yes, false},
		"permisos resaltados tecleando otra cosa":   {elevatedPreview(), Acceptance{Confirmed: true, Typed: "si"}, false},
		"permisos resaltados tecleando el ambiente": {elevatedPreview(), Acceptance{Typed: "sand"}, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newBootstrapRig(t)
			r.provider.preview = tt.preview
			plan := r.plan(t, BootstrapOptions{Region: "sur"})
			r.log = nil

			_, err := r.service.Apply(context.Background(), plan, tt.acceptance)

			if tt.accepted {
				require.NoError(t, err)
				assert.Contains(t, r.log, "apply:sello-1")
				return
			}
			require.ErrorIs(t, err, ErrBootstrapNotAccepted)
			assert.Empty(t, r.log, "ni la nube ni el portal se tocan")
			assert.Empty(t, r.journal.records)
		})
	}
}

func TestBootstrap_LosPermisosResaltadosQuedanEnLaConstancia(t *testing.T) {
	r := newBootstrapRig(t)
	r.provider.preview = elevatedPreview()
	r.provider.preview.Changes = []PlannedChange{
		{Action: ChangeUnknown, Kind: "despliegue", Name: "carga"},
		{Action: ChangeCreate, Grant: &PlannedGrant{Identity: "x", Role: "Dueño", Scope: "cuenta", Elevated: true}},
		{Action: ChangeNone, Grant: &PlannedGrant{Identity: "y", Role: "Dueño", Scope: "cuenta", Elevated: true}},
	}
	plan := r.plan(t, BootstrapOptions{Region: "sur"})
	assert.Equal(t, "sand", plan.TypedConfirmation())

	_, err := r.service.Apply(context.Background(), plan, Acceptance{Typed: "sand"})

	require.NoError(t, err)
	record := r.journal.records[0]
	assert.Len(t, record.ElevatedGrants, 2, "el declarado y el que la vista previa crearía; el que ya existe no")
	assert.Equal(t, 1, record.Unpreviewed)
	assert.Equal(t, []LabeledValue{{Label: "Cuenta", Value: "ana"}}, record.Account)
}

func TestBootstrap_LoQueDetieneLaAplicacion(t *testing.T) {
	t.Run("el bootstrap cambió después de la vista previa", func(t *testing.T) {
		r := newBootstrapRig(t)
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		r.bootstraps.bootstrap.Fingerprint = strings.Repeat("f", 64)
		r.log = nil

		_, err := r.service.Apply(context.Background(), plan, yes)

		require.ErrorIs(t, err, ErrBootstrapChanged)
		assert.Empty(t, r.log)
		assert.Equal(t, "/cache/pipe.git|nube", r.bootstraps.reads[len(r.bootstraps.reads)-1], "se vuelve a leer la misma carpeta")
	})
	t.Run("no se puede dejar constancia", func(t *testing.T) {
		r := newBootstrapRig(t)
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		r.journal.err = errors.New("disco lleno")
		r.log = nil

		_, err := r.service.Apply(context.Background(), plan, yes)

		assert.ErrorContains(t, err, "disco lleno")
		assert.Empty(t, r.log)
	})
	t.Run("falla la plantilla: no hay confianza ni complete", func(t *testing.T) {
		r := newBootstrapRig(t)
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		r.provider.applyErr = errors.New("sin permisos")
		r.log = nil

		_, err := r.service.Apply(context.Background(), plan, yes)

		assert.ErrorContains(t, err, "sin permisos")
		assert.Equal(t, []string{"apply:sello-1"}, r.log)
	})
	t.Run("la identidad ya confía en otro: no se completa", func(t *testing.T) {
		r := newBootstrapRig(t)
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		r.trust.ensureErr = errors.New("ya confía en otros emisores")
		r.log = nil

		_, err := r.service.Apply(context.Background(), plan, yes)

		assert.ErrorContains(t, err, "otros emisores")
		assert.Equal(t, []string{"apply:sello-1", "trust.exposure", "trust.ensure"}, r.log)
	})
}

type throttled struct{ wait time.Duration }

func (e throttled) Error() string             { return "límite" }
func (e throttled) Unwrap() error             { return ErrCloudTokenThrottled }
func (e throttled) RetryAfter() time.Duration { return e.wait }

func TestBootstrap_Verificacion(t *testing.T) {
	verifying := answer{v: Verification{Status: VerificationVerifying, RetryAfter: 10 * time.Second, Message: "aún no"}}
	active := answer{v: Verification{Status: VerificationActive}}

	apply := func(t *testing.T, answers ...answer) (*bootstrapRig, error) {
		t.Helper()
		r := newBootstrapRig(t)
		r.admin.answers = answers
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		_, err := r.service.Apply(context.Background(), plan, yes)
		return r, err
	}

	t.Run("espera lo que pide el portal hasta que se activa", func(t *testing.T) {
		r, err := apply(t, verifying, verifying, active)

		require.NoError(t, err)
		assert.Equal(t, []time.Duration{10 * time.Second, 10 * time.Second}, r.slept)
		assert.Len(t, r.admin.completions, 3, "complete es idempotente: se repite el mismo")
		assert.Contains(t, strings.Join(r.notify.infos, "\n"), "aún no", "el progreso se ve")
	})
	t.Run("fallo definitivo", func(t *testing.T) {
		r, err := apply(t, answer{v: Verification{Status: VerificationFailed, Message: "La identidad no existe.", Hint: "vex bootstrap sand --reconnect"}})

		var failed *VerificationFailedError
		require.ErrorAs(t, err, &failed)
		assert.ErrorContains(t, err, "La identidad no existe. Ejecuta `vex bootstrap sand --reconnect`")
		assert.Equal(t, []string{"bootstrap:accepted", "bootstrap:not_verified"}, r.journal.outcomes())
	})
	t.Run("si el portal ya dice qué ejecutar no se repite", func(t *testing.T) {
		message := "Azure no reconoce la confianza. Ejecuta `vex bootstrap sand --reconnect`."
		_, err := apply(t, answer{v: Verification{Status: VerificationFailed, Message: message, Hint: "vex bootstrap sand --reconnect"}})

		assert.EqualError(t, err, "La conexión de nube de 'sand' no pasó la verificación: "+message)
	})
	t.Run("límite del portal: espera Retry-After, acotado", func(t *testing.T) {
		r, err := apply(t, answer{err: throttled{wait: 30 * time.Second}}, answer{err: throttled{wait: time.Hour}}, active)

		require.NoError(t, err)
		assert.Equal(t, []time.Duration{30 * time.Second, time.Minute}, r.slept)
	})
	t.Run("otra verificación en curso: reintenta", func(t *testing.T) {
		r, err := apply(t, answer{err: &ConnectionRejectedError{Code: RejectionConnectionInProgress}}, active)

		require.NoError(t, err)
		assert.Len(t, r.slept, 1)
	})
	t.Run("hay una conexión más nueva: no se reintenta", func(t *testing.T) {
		r, err := apply(t, answer{err: &ConnectionRejectedError{Code: RejectionConnectionExists}})

		assert.ErrorContains(t, err, "vuelve a ejecutar `vex bootstrap sand`")
		assert.Empty(t, r.slept)
	})
	t.Run("un corte de red no deshace nada: se vuelve a preguntar", func(t *testing.T) {
		r, err := apply(t, answer{err: errors.New("connection reset")}, active)

		require.NoError(t, err)
		assert.Len(t, r.slept, 1)
		assert.NotEmpty(t, r.notify.warnings)
	})
	t.Run("la sesión de vex dejó de valer: se detiene", func(t *testing.T) {
		_, err := apply(t, answer{err: errors.Join(ErrCloudTokenDenied, ErrPortalSessionInvalid)})

		require.ErrorIs(t, err, ErrPortalSessionInvalid)
	})
	t.Run("no llega a verificarse en el plazo", func(t *testing.T) {
		r, err := apply(t, verifying)

		require.ErrorIs(t, err, ErrVerificationPending)
		assert.ErrorContains(t, err, "vuelve a ejecutar `vex bootstrap sand` para retomarlo")
		assert.LessOrEqual(t, len(r.slept), int(verifyBudget/(10*time.Second)), "la espera está acotada")
	})
	t.Run("re-verificación no concluyente de una activa: sigue preguntando", func(t *testing.T) {
		r, err := apply(t, answer{v: Verification{Status: VerificationActive, DetailCode: "upstream_unavailable", Message: "Microsoft no respondió"}}, active)

		require.NoError(t, err)
		assert.Len(t, r.slept, 1)
	})
}

func TestBootstrap_CtrlCDejaLaConexionReanudable(t *testing.T) {
	r := newBootstrapRig(t)
	r.admin.answers = []answer{{v: Verification{Status: VerificationVerifying, RetryAfter: time.Second, Message: "aún no"}}}
	plan := r.plan(t, BootstrapOptions{Region: "sur"})
	ctx, cancel := context.WithCancel(context.Background())
	r.service.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := r.service.Apply(ctx, plan, yes)

	require.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "Vuelve a ejecutar `vex bootstrap sand` para retomarla")
}

// --- check ---

func checkTexts(report CheckReport, status CheckStatus) []string {
	var texts []string
	for _, item := range report.Items {
		if item.Status == status {
			texts = append(texts, item.Text)
		}
	}
	return texts
}

func newCheckRig(t *testing.T) *bootstrapRig {
	t.Helper()
	r := newBootstrapRig(t)
	r.activeConnection()
	r.provider.preview = BootstrapPreview{Changes: []PlannedChange{{Action: ChangeNone}, {Action: ChangeIgnored}}}
	r.trust.findings = []TrustFinding{{OK: true, Text: "solo confía en vex"}, {OK: true, Text: "credencial correcta"}}
	return r
}

func TestCheck_TodoEnOrdenYSoloLee(t *testing.T) {
	r := newCheckRig(t)

	report, err := r.service.Check(context.Background(), "sand")

	require.NoError(t, err)
	assert.False(t, report.Failed())
	assert.Len(t, checkTexts(report, CheckOK), 7, "huella, destino, dos de confianza, permisos de la identidad, vista previa y canje")
	assert.Equal(t, []string{
		"validate", "account", "trust.inspect:" + testSubject, "preview", "trust.exposure", "verify:" + testConnectionID,
	}, r.log, "ni apply, ni register, ni ensure, ni remove, ni create")
	assert.Equal(t, "sur", r.provider.requests[0].Region, "la vista previa usa la región de la conexión")
	assert.Equal(t, "vex-bootstrap-demo-b5a1c2-sand", r.provider.requests[0].Name)
	assert.Empty(t, r.journal.records)
}

func TestCheck_Hallazgos(t *testing.T) {
	tests := map[string]struct {
		change func(*bootstrapRig)
		want   string
	}{
		"la huella cambió": {func(r *bootstrapRig) { r.conns.info.BootstrapSHA256 = strings.Repeat("0", 64) },
			"El bootstrap del pipeline cambió"},
		"otra credencial en la identidad": {func(r *bootstrapRig) {
			r.trust.findings[0] = TrustFinding{Text: "confía en emisores que vex no puso (intruso)"}
		},
			"intruso"},
		"la vista previa trae cambios": {func(r *bootstrapRig) {
			r.provider.preview.Changes = append(r.provider.preview.Changes, PlannedChange{Action: ChangeCreate}, PlannedChange{Action: ChangeModify})
		}, "1 por crear, 1 por modificar"},
		"la vista previa rompe una regla": {func(r *bootstrapRig) {
			r.provider.previewErr = &BootstrapRejectedError{Rule: "V1", Detail: "borraría rg"}
		},
			"regla V1"},
		"el canje falla": {func(r *bootstrapRig) {
			r.admin.answers = []answer{{v: Verification{Status: VerificationFailed, Message: "La confianza fue modificada."}}}
		}, "El canje de un token de vex no funciona: La confianza fue modificada."},
		"el portal no deja verificar": {func(r *bootstrapRig) {
			r.admin.answers = []answer{{err: &ConnectionRejectedError{Code: RejectionConnectionExists}}}
		}, "No se pudo probar el canje"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newCheckRig(t)
			tt.change(r)

			report, err := r.service.Check(context.Background(), "sand")

			require.NoError(t, err)
			assert.True(t, report.Failed())
			failed := checkTexts(report, CheckFailed)
			require.Len(t, failed, 1, "solo falla lo que se rompió")
			assert.Contains(t, failed[0], tt.want)
		})
	}
}

func TestCheck_LoQueNoSePuedeComprobarSeDice(t *testing.T) {
	t.Run("sesión en otro destino: no se audita la nube, pero sí el canje", func(t *testing.T) {
		r := newCheckRig(t)
		r.provider.account.Target = map[string]string{"cuenta": "999"}

		report, err := r.service.Check(context.Background(), "sand")

		require.NoError(t, err)
		assert.True(t, report.Failed())
		assert.Contains(t, checkTexts(report, CheckFailed)[0], "apunta a otro destino")
		assert.Equal(t, []string{"validate", "account", "verify:" + testConnectionID}, r.log)
	})
	t.Run("recursos sin vista previa y canje no concluyente: avisos, no fallos", func(t *testing.T) {
		r := newCheckRig(t)
		r.provider.preview.Changes = append(r.provider.preview.Changes, PlannedChange{Action: ChangeUnknown})
		r.admin.answers = []answer{{v: Verification{Status: VerificationActive, DetailCode: "upstream_unavailable", Message: "Microsoft no respondió."}}}

		report, err := r.service.Check(context.Background(), "sand")

		require.NoError(t, err)
		assert.False(t, report.Failed())
		assert.Len(t, checkTexts(report, CheckWarning), 2)
	})
	t.Run("sin conexión activa no hay nada que auditar", func(t *testing.T) {
		r := newCheckRig(t)
		r.conns.info = CloudConnectionInfo{Status: "revoked"}

		_, err := r.service.Check(context.Background(), "sand")

		var notReady *ConnectionNotReadyError
		require.ErrorAs(t, err, &notReady)
		assert.NotContains(t, r.log, "preview")
	})
}

// --- disconnect ---

func disconnectPlan(t *testing.T, r *bootstrapRig) *DisconnectPlan {
	t.Helper()
	plan, err := r.disconnect.Plan(context.Background(), "sand")
	require.NoError(t, err)
	return plan
}

func TestDisconnect_RevocaYRetiraLaConfianza(t *testing.T) {
	r := newBootstrapRig(t)
	r.activeConnection()
	plan := disconnectPlan(t, r)
	assert.Empty(t, r.log, "planear no toca nada")

	result, err := r.disconnect.Run(context.Background(), plan)

	require.NoError(t, err)
	assert.True(t, result.TrustRemoved)
	assert.Equal(t, []string{"revoke", "account", "trust.remove"}, r.log, "primero el portal")
	assert.Equal(t, []string{testConnectionID}, r.admin.revoked)
	assert.Equal(t, []Trust{{Issuer: testIssuer, Subject: testSubject, Audience: testAudience}}, r.trust.removed)
	assert.Empty(t, result.Manual)
	assert.Equal(t, []string{"nube grupo borrar"}, result.Cleanup, "los comandos de limpieza se entregan, no se ejecutan")
	assert.Equal(t, []string{"disconnect:trust_removed"}, r.journal.outcomes())
}

func TestDisconnect_SiElPortalNoConfirmaNoSeTocaLaNube(t *testing.T) {
	r := newBootstrapRig(t)
	r.activeConnection()
	plan := disconnectPlan(t, r)
	r.admin.revokeErr = errors.New("503")

	result, err := r.disconnect.Run(context.Background(), plan)

	assert.ErrorContains(t, err, "no confirmó la revocación; no se tocó nada en la nube")
	assert.Equal(t, []string{"revoke"}, r.log)
	assert.False(t, result.TrustRemoved)
	assert.Empty(t, r.journal.records)
}

func TestDisconnect_CuandoNoPuedeRetirarLaConfianzaDiceComo(t *testing.T) {
	tests := map[string]struct {
		change  func(*bootstrapRig)
		pending string
		manual  []string
		log     []string
	}{
		"sin sesión de la nube": {func(r *bootstrapRig) { r.provider.accountErr = errors.New("ejecuta `nube login`") },
			"no hay una sesión de la nube", []string{"nube confianza borrar"}, []string{"revoke", "account"}},
		"sesión en otro destino": {func(r *bootstrapRig) { r.provider.account.Target = map[string]string{"cuenta": "999"} },
			"apunta a otro destino", []string{"nube confianza borrar"}, []string{"revoke", "account"}},
		"un bloqueo lo impide": {func(r *bootstrapRig) {
			r.trust.removeErr = &TrustLockedError{Commands: []string{"quitar bloqueo", "borrar", "poner bloqueo"}}
		}, "vex no lo quita por su cuenta", []string{"quitar bloqueo", "borrar", "poner bloqueo"}, []string{"revoke", "account", "trust.remove"}},
		"la nube falla": {func(r *bootstrapRig) { r.trust.removeErr = errors.New("sin permisos") },
			"sin permisos", []string{"nube confianza borrar"}, []string{"revoke", "account", "trust.remove"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newBootstrapRig(t)
			r.activeConnection()
			tt.change(r)

			result, err := r.disconnect.Run(context.Background(), disconnectPlan(t, r))

			require.NoError(t, err, "la revocación sí se hizo")
			assert.False(t, result.TrustRemoved)
			assert.Contains(t, result.Pending, tt.pending)
			assert.Equal(t, tt.manual, result.Manual)
			assert.Equal(t, tt.log, r.log)
			assert.Equal(t, []string{"disconnect:revoked_only"}, r.journal.outcomes())
		})
	}
}

func TestDisconnect_ConexionesQueNoEstanActivas(t *testing.T) {
	t.Run("nada que desconectar", func(t *testing.T) {
		for _, info := range []CloudConnectionInfo{{Status: "absent"}, {Status: "revoked", ConnectionID: testConnectionID}} {
			r := newBootstrapRig(t)
			r.conns.info = info

			_, err := r.disconnect.Plan(context.Background(), "sand")

			require.ErrorIs(t, err, ErrNoCloudConnection, info.Status)
		}
	})
	t.Run("a medias: se revoca, y la nube queda para la persona", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.conns.info = CloudConnectionInfo{Status: "verifying", ConnectionID: testConnectionID}

		result, err := r.disconnect.Run(context.Background(), disconnectPlan(t, r))

		require.NoError(t, err)
		assert.Equal(t, []string{"revoke"}, r.log)
		assert.Contains(t, result.Pending, "no llegó a activarse")
	})
}

// Barandilla de la identidad (hallazgo A1 de la revisión de seguridad).

func TestBootstrap_UnaIdentidadQueElBootstrapNoCreoNoRecibeLaConfianza(t *testing.T) {
	r := newBootstrapRig(t)
	r.trust.exposureErr = &BootstrapRejectedError{Rule: "V6", Detail: "la identidad no aparece en la vista previa"}
	plan := r.plan(t, BootstrapOptions{Region: "sur"})
	r.log = nil

	_, err := r.service.Apply(context.Background(), plan, yes)

	var rejected *BootstrapRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, []string{"apply:sello-1", "trust.exposure"}, r.log, "ni confianza ni conexión completada")
	assert.Empty(t, r.trust.ensured)
	assert.Equal(t, plan.Preview, r.trust.previews[0], "se contrasta con la vista previa que se aceptó")
}

func TestBootstrap_PermisosQueLaVistaPreviaNoMostroSeAceptanPorEscrito(t *testing.T) {
	owner := PlannedGrant{Identity: "identidad id-1", Role: "Owner", Scope: "toda la suscripción", Elevated: true, Reason: "la vista previa no lo mostró"}
	answering := func(typed string, asked *[]PlannedGrant) Acceptance {
		return Acceptance{Confirmed: true, ConfirmExposure: func(grants []PlannedGrant) (string, error) {
			*asked = grants
			return typed, nil
		}}
	}
	t.Run("sin quien pregunte no se crea la confianza", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.trust.exposed = []PlannedGrant{owner}
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		r.log = nil

		_, err := r.service.Apply(context.Background(), plan, yes)

		require.ErrorIs(t, err, ErrBootstrapNotAccepted)
		assert.ErrorContains(t, err, "vex no creó su confianza")
		assert.Equal(t, []string{"apply:sello-1", "trust.exposure"}, r.log)
		assert.Equal(t, []string{"bootstrap:accepted", "bootstrap:exposure_declined"}, r.journal.outcomes())
	})
	t.Run("un sí no basta: hay que teclear el ambiente", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.trust.exposed = []PlannedGrant{owner}
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		var asked []PlannedGrant

		_, err := r.service.Apply(context.Background(), plan, answering("si", &asked))

		require.ErrorIs(t, err, ErrBootstrapNotAccepted)
		assert.Equal(t, []PlannedGrant{owner}, asked)
		assert.Empty(t, r.trust.ensured)
	})
	t.Run("aceptados por escrito: se crea la confianza y queda constancia", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.trust.exposed = []PlannedGrant{owner}
		plan := r.plan(t, BootstrapOptions{Region: "sur"})
		var asked []PlannedGrant
		r.log = nil

		_, err := r.service.Apply(context.Background(), plan, answering("sand", &asked))

		require.NoError(t, err)
		assert.Equal(t, []string{"apply:sello-1", "trust.exposure", "trust.ensure", "complete"}, r.log)
		assert.Equal(t, []string{"bootstrap:accepted", "bootstrap:exposure_accepted", "bootstrap:active"}, r.journal.outcomes())
		assert.Equal(t, []PlannedGrant{owner}, r.journal.records[1].ExposedGrants)
	})
	t.Run("también al reconectar", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.activeConnection()
		r.trust.exposed = []PlannedGrant{owner}
		plan := r.plan(t, BootstrapOptions{Reconnect: true})
		require.Equal(t, BootstrapReconnect, plan.Mode)

		_, err := r.service.Apply(context.Background(), plan, yes)

		require.ErrorIs(t, err, ErrBootstrapNotAccepted)
		assert.Empty(t, r.trust.ensured)
	})
}

func TestBootstrap_LoQueNoSePudoPrevisualizarSeAceptaPorEscrito(t *testing.T) {
	// B3: aceptar a ciegas no puede ser un «sí» por inercia.
	r := newBootstrapRig(t)
	r.provider.preview.Changes = append(r.provider.preview.Changes, PlannedChange{Action: ChangeUnknown, Kind: "despliegue", Name: "oculto"})
	plan := r.plan(t, BootstrapOptions{Region: "sur"})

	assert.Equal(t, "sand", plan.TypedConfirmation())
	_, err := r.service.Apply(context.Background(), plan, yes)
	require.ErrorIs(t, err, ErrBootstrapNotAccepted)

	_, err = r.service.Apply(context.Background(), plan, Acceptance{Typed: "sand"})
	require.NoError(t, err)
}

func TestCheck_LaIdentidadYSusPermisos(t *testing.T) {
	t.Run("la identidad de la conexión no es la del bootstrap", func(t *testing.T) {
		r := newCheckRig(t)
		r.trust.exposureErr = &BootstrapRejectedError{Rule: "V6", Detail: "la identidad no aparece en la vista previa"}

		report, err := r.service.Check(context.Background(), "sand")

		require.NoError(t, err)
		assert.True(t, report.Failed())
		assert.Contains(t, strings.Join(checkTexts(report, CheckFailed), "\n"), "no es la que crea el bootstrap")
	})
	t.Run("la identidad tiene permisos que el bootstrap no declara", func(t *testing.T) {
		r := newCheckRig(t)
		r.trust.exposed = []PlannedGrant{{Identity: "identidad id-1", Role: "Owner", Scope: "toda la suscripción", Elevated: true}}

		report, err := r.service.Check(context.Background(), "sand")

		require.NoError(t, err)
		assert.True(t, report.Failed())
		assert.Contains(t, strings.Join(checkTexts(report, CheckFailed), "\n"), "Owner → toda la suscripción")
	})
	t.Run("no se pudieron leer", func(t *testing.T) {
		r := newCheckRig(t)
		r.trust.exposureErr = errors.New("AuthorizationFailed")

		report, err := r.service.Check(context.Background(), "sand")

		require.NoError(t, err)
		assert.True(t, report.Failed())
	})
}
