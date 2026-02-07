package application

import (
	"context"
	"fmt"
	defAgg "github.com/jairoprogramador/vex/internal/domain/definition/aggregates"
	defPrt "github.com/jairoprogramador/vex/internal/domain/definition/ports"
	defVos "github.com/jairoprogramador/vex/internal/domain/definition/vos"
	exePrt "github.com/jairoprogramador/vex/internal/domain/execution/ports"
	exeVos "github.com/jairoprogramador/vex/internal/domain/execution/vos"
	proAgg "github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proPrt "github.com/jairoprogramador/vex/internal/domain/project/ports"
	staPrt "github.com/jairoprogramador/vex/internal/domain/state/ports"
	staVos "github.com/jairoprogramador/vex/internal/domain/state/vos"
	verPrt "github.com/jairoprogramador/vex/internal/domain/versioning/ports"
	worAgg "github.com/jairoprogramador/vex/internal/domain/workspace/aggregates"
)

// ExecutionOrchestrator orquesta la ejecución de un plan completo,
// coordinando los contextos de definición, estado y ejecución.
type ExecutionOrchestrator struct {
	projectPath       string
	rootVexPath       string
	projectSvc        *ProjectService
	workspaceSvc      *WorkspaceService
	gitCloner         proPrt.ClonerTemplate
	versionCalculator verPrt.VersionCalculator
	planBuilder       defPrt.PlanBuilder
	fingerprintSvc    staPrt.FingerprintService
	stateManager      staPrt.StateManager
	stepExecutor      exePrt.StepExecutor
	copyWorkdir       exePrt.CopyWorkdir
	varsRepository    exePrt.VarsRepository
	gitRepository     verPrt.GitRepository
}

// NewExecutionOrchestrator crea una nueva instancia del orquestador.
func NewExecutionOrchestrator(
	projectPath string,
	rootVexPath string,
	projectSvc *ProjectService,
	workspaceSvc *WorkspaceService,
	gitCloner proPrt.ClonerTemplate,
	versionCalculator verPrt.VersionCalculator,
	planBuilder defPrt.PlanBuilder,
	fingerprintSvc staPrt.FingerprintService,
	stateManager staPrt.StateManager,
	stepExecutor exePrt.StepExecutor,
	copyWorkdir exePrt.CopyWorkdir,
	varsRepository exePrt.VarsRepository,
	gitRepository verPrt.GitRepository,
) *ExecutionOrchestrator {
	return &ExecutionOrchestrator{
		projectPath:       projectPath,
		rootVexPath:       rootVexPath,
		projectSvc:        projectSvc,
		workspaceSvc:      workspaceSvc,
		gitCloner:         gitCloner,
		versionCalculator: versionCalculator,
		planBuilder:       planBuilder,
		fingerprintSvc:    fingerprintSvc,
		stateManager:      stateManager,
		stepExecutor:      stepExecutor,
		copyWorkdir:       copyWorkdir,
		varsRepository:    varsRepository,
		gitRepository:     gitRepository,
	}
}

// ExecutePlan es el caso de uso principal que ejecuta un plan de despliegue.
func (o *ExecutionOrchestrator) ExecutePlan(ctx context.Context, stepName, envName string) error {
	// 1. Inicializar, Cargar y Clonar
	project, err := o.loadProject(ctx, o.projectPath)
	if err != nil {
		return err
	}
	workspace, err := o.loadWorkspace(project, o.rootVexPath)
	if err != nil {
		return err
	}

	templateLocalPath := workspace.TemplatePath()
	err = o.cloneTemplate(ctx, project, templateLocalPath)
	if err != nil {
		return err
	}

	planDef, err := o.buildPlan(ctx, templateLocalPath, stepName, envName)
	if err != nil {
		return err
	}

	version, commit, err := o.versionCalculator.CalculateNextVersion(ctx, o.projectPath, false)
	if err != nil {
		return err
	}

	environment := planDef.Environment().String()

	projectVars := o.prepareProjectVariables(project)
	othersVars := o.prepareOthersVariables(
		environment, o.projectPath, version.String(), commit.String())

	cumulativeVars := make(exeVos.VariableSet)
	cumulativeVars.AddAll(projectVars)
	cumulativeVars.AddAll(othersVars)

	fmt.Println("Iniciando la ejecución del plan...")
	fmt.Printf("  - Entorno: %s\n", environment)
	fmt.Printf("  - Versión: %s\n", version.String())
	fmt.Printf("  - Commit: %s\n", commit.String())

	// 3. Bucle de Ejecución Paso a Paso
	for _, stepDef := range planDef.Steps() {

		fmt.Printf("Ejecutando paso %s ...\n", stepDef.NameDef().Name())

		fingerprints, err := o.generateStepFingerprints(o.projectPath, environment, workspace, stepDef.NameDef())
		if err != nil {
			return fmt.Errorf("error al generar fingerprint para el paso '%s': %w", stepDef.NameDef().Name(), err)
		}

		stateTablePath, err := workspace.StateTablePath(stepDef.NameDef().Name())
		if err != nil {
			return fmt.Errorf("error al obtener la ruta del estado del paso '%s': %w", stepDef.NameDef().Name(), err)
		}
		hasChanged, err := o.stateManager.HasStateChanged(stateTablePath, fingerprints, staVos.NewCachePolicy(0))
		if err != nil {
			return fmt.Errorf("error al comprobar el estado del paso '%s': %w", stepDef.NameDef().Name(), err)
		}

		varsStepPath := workspace.VarsFilePath(environment, stepDef.NameDef().Name())
		varsStep, err := o.varsRepository.Get(varsStepPath)
		if err != nil {
			return fmt.Errorf("error al obtener las variables del paso '%s' en el entorno '%s': %w", stepDef.NameDef().Name(), environment, err)
		}
		cumulativeVars.AddAll(varsStep)

		varsSharedPath := workspace.VarsFilePath("shared", stepDef.NameDef().Name())
		varsShared, err := o.varsRepository.Get(varsSharedPath)
		if err != nil {
			return fmt.Errorf("error al obtener las variables del paso '%s' en el entorno 'shared': %w", stepDef.NameDef().Name(), err)
		}
		cumulativeVars.AddAll(varsShared)

		if !hasChanged {
			fmt.Printf("  - Paso '%s' ya fue ejecutado en este entorno. Omitiendo.\n", stepDef.NameDef().Name())
			continue // Saltar al siguiente paso
		}

		envStepPath := workspace.ScopeWorkdirPath(planDef.Environment().String(), stepDef.NameDef().Name())
		err = o.copyWorkdir.Copy(ctx, workspace.StepTemplatePath(stepDef.NameDef().FullName()), envStepPath, false)
		if err != nil {
			return fmt.Errorf("error al copiar el paso '%s' al workspace: %w", envStepPath, err)
		}

		sharedStepPath := workspace.ScopeWorkdirPath(exeVos.SharedScope, stepDef.NameDef().Name())
		err = o.copyWorkdir.Copy(ctx, workspace.StepTemplatePath(stepDef.NameDef().FullName()), sharedStepPath, true)
		if err != nil {
			return fmt.Errorf("error al copiar el paso '%s' al workspace: %w", sharedStepPath, err)
		}

		execStep, err := mapToExecutionStep(stepDef, envStepPath, sharedStepPath)
		if err != nil {
			return fmt.Errorf("error al mapear la definición del paso '%s': %w", stepDef.NameDef().Name(), err)
		}

		execResult, err := o.stepExecutor.Execute(ctx, execStep, cumulativeVars)
		if err != nil {
			return fmt.Errorf("la ejecución del paso '%s' falló: %w", stepDef.NameDef().Name(), err)
		}
		if execResult.Error != nil || execResult.Status == exeVos.Failure {
			fmt.Println("--- Logs del fallo ---")
			fmt.Println(execResult.Logs)
			fmt.Println("--------------------")
			return fmt.Errorf("el paso '%s' finalizó con error: %w", stepDef.NameDef().Name(), execResult.Error)
		}

		// 3c. Actualización de Variables y Estado
		fmt.Printf("  - Paso '%s' completado:\n%s\n", stepDef.NameDef().Name(), execResult.Logs)
		cumulativeVars.AddAll(execResult.OutputVars)

		outputSharedVars := execResult.OutputVars.Filter(func(v exeVos.OutputVar) bool {
			return v.IsShared()
		})
		if !outputSharedVars.Equals(varsShared) {
			err := o.varsRepository.Save(varsSharedPath, outputSharedVars)
			if err != nil {
				return fmt.Errorf("error al guardar las variables del paso '%s' del entorno '%s': %w", stepName, environment, err)
			}
		}

		outputStepVars := execResult.OutputVars.Filter(func(v exeVos.OutputVar) bool {
			return !v.IsShared()
		})
		if !outputStepVars.Equals(varsStep) {
			err := o.varsRepository.Save(varsStepPath, outputStepVars)
			if err != nil {
				return fmt.Errorf("error al guardar las variables del paso '%s' del entorno '%s': %w", stepName, environment, err)
			}
		}

		if err := o.stateManager.UpdateState(stateTablePath, fingerprints); err != nil {
			// Esto es una advertencia. El flujo principal fue exitoso, pero el estado no se guardó.
			fmt.Printf("ADVERTENCIA: no se pudo guardar el estado del paso '%s'. Se re-ejecutará la próxima vez. Error: %v\n", stepDef.NameDef().Name(), err)
		}
	}

	if stepName == "deploy" {
		// 4. Crear el tag del commit
		err = o.gitRepository.CreateTagForCommit(ctx, o.projectPath, commit.String(), version.String())
		if err != nil {
			fmt.Printf("ADVERTENCIA: no se pudo crear el tag del commit. Error: %v\n", err)
		}
	}

	fmt.Println("¡Ejecución completada con éxito!")
	return nil
}

func (o *ExecutionOrchestrator) loadProject(ctx context.Context, projectPath string) (*proAgg.Project, error) {
	// 1. Cargar el Proyecto
	project, err := o.projectSvc.Load(ctx, projectPath)
	if err != nil {
		return nil, fmt.Errorf("error al cargar el proyecto: %w", err)
	}
	return project, nil
}

func (o *ExecutionOrchestrator) loadWorkspace(project *proAgg.Project, rootVexPath string) (*worAgg.Workspace, error) {
	// 2. Crear el Workspace
	workspace, err := o.workspaceSvc.NewWorkspace(
		rootVexPath, project.Data().Name(), project.TemplateRepo().DirName())
	if err != nil {
		return nil, fmt.Errorf("error al cargar el workspace: %w", err)
	}
	return workspace, nil
}

func (o *ExecutionOrchestrator) cloneTemplate(
	ctx context.Context, project *proAgg.Project, templateLocalPath string) error {
	// 3. Asegurar que el template está clonado
	err := o.gitCloner.EnsureCloned(ctx, project.TemplateRepo().URL(),
		project.TemplateRepo().Ref(), templateLocalPath)
	if err != nil {
		return fmt.Errorf("no se pudo clonar el repositorio de plantillas: %w", err)
	}
	return nil
}

func (o *ExecutionOrchestrator) buildPlan(
	ctx context.Context, templateLocalPath, stepName, envName string) (*defAgg.ExecutionPlanDefinition, error) {

	// 4. Cargar la definición del plan desde el template clonado
	planDef, err := o.planBuilder.Build(ctx, templateLocalPath, stepName, envName)
	if err != nil {
		return nil, fmt.Errorf("error al cargar la definición: %w", err)
	}

	return planDef, nil
}

func (o *ExecutionOrchestrator) prepareProjectVariables(project *proAgg.Project) exeVos.VariableSet {
	vars := exeVos.NewVariableSetFromMap(map[string]string{
		"project_id":           project.ID().String()[:8],
		"project_name":         project.Data().Name(),
		"project_organization": project.Data().Organization(),
		"project_team":         project.Data().Team(),
	})
	return vars
}

func (o *ExecutionOrchestrator) prepareOthersVariables(environment, projectWorkdir, version, commit string) exeVos.VariableSet {
	vars := exeVos.NewVariableSetFromMap(map[string]string{
		"project_version":       version,
		"project_revision":      commit[:8],
		"project_revision_full": commit,
		"environment":           environment,
		"project_workdir":       projectWorkdir,
		"tool_name":             "vex",
	})
	return vars
}

func (o *ExecutionOrchestrator) generateCodeFingerprint(projectPath string) (staVos.Fingerprint, error) {
	codeFp, err := o.fingerprintSvc.FromDirectory(projectPath)
	if err != nil {
		return staVos.Fingerprint{}, fmt.Errorf("no se pudo generar el fingerprint para el proyecto: %w", err)
	}
	return codeFp, nil
}

func (o *ExecutionOrchestrator) generateInstructionFingerprint(templateInstPath string) (staVos.Fingerprint, error) {
	codeFp, err := o.fingerprintSvc.FromDirectory(templateInstPath)
	if err != nil {
		return staVos.Fingerprint{}, fmt.Errorf("no se pudo generar el fingerprint para las instrucciones: %w", err)
	}
	return codeFp, nil
}

func (o *ExecutionOrchestrator) generateVarsFingerprint(templateVarsPath string) (staVos.Fingerprint, error) {
	codeFp, err := o.fingerprintSvc.FromFile(templateVarsPath)
	if err != nil {
		return staVos.Fingerprint{}, fmt.Errorf("no se pudo generar el fingerprint para las variables: %w", err)
	}
	return codeFp, nil
}

func (o *ExecutionOrchestrator) generateStepFingerprints(
	projectPath, environment string,
	workspace *worAgg.Workspace,
	stepDef defVos.StepNameDefinition) (staVos.CurrentStateFingerprints, error) {

	envFp, err := staVos.NewEnvironment(environment)
	if err != nil {
		return staVos.CurrentStateFingerprints{}, err
	}

	codeFp, err := o.generateCodeFingerprint(projectPath)
	if err != nil {
		return staVos.CurrentStateFingerprints{}, err
	}

	instructionPath := workspace.StepTemplatePath(stepDef.FullName())
	instFp, err := o.generateInstructionFingerprint(instructionPath)
	if err != nil {
		return staVos.CurrentStateFingerprints{}, err
	}

	varsPath := workspace.VarsTemplatePath(environment, stepDef.Name())
	varsFp, err := o.generateVarsFingerprint(varsPath)
	if err != nil {
		return staVos.CurrentStateFingerprints{}, err
	}

	return staVos.NewCurrentStateFingerprints(codeFp, instFp, varsFp, envFp), nil
}
