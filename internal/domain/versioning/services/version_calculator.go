package services

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jairoprogramador/vex/internal/domain/versioning/ports"
	"github.com/jairoprogramador/vex/internal/domain/versioning/vos"
)

// Regexp para parsear tags SemVer como v1.2.3
var semverTagRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Regexp para parsear tipos de commit convencionales
var conventionalCommitRegex = regexp.MustCompile(`^(feat|fix|build|chore|ci|docs|style|refactor|perf|test)(\(.*\))?(!?):`)

// changeLevel define el nivel de cambio semántico.
type changeLevel int

// Definimos constantes para la precedencia de los cambios.
const (
	changeNone changeLevel = iota
	changePatch
	changeMinor
	changeMajor
)

// VersionCalculatorService calcula la siguiente versión semántica basada en el historial de commits.
type VersionCalculatorService struct {
	gitRepo ports.GitRepository
}

// NewVersionCalculator crea una nueva instancia de VersionCalculator.
func NewVersionCalculator(gitRepo ports.GitRepository) ports.VersionCalculator {
	return &VersionCalculatorService{gitRepo: gitRepo}
}

// CalculateNextVersion determina la siguiente versión y el commit actual.
// Si 'forceDateVersion' es true, devuelve una versión basada en la fecha actual.
func (vc *VersionCalculatorService) CalculateNextVersion(ctx context.Context, repoPath string, forceDateVersion bool) (*vos.Version, *vos.Commit, error) {
	lastCommit, err := vc.gitRepo.GetLastCommit(ctx, repoPath)
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudo obtener el último commit: %w", err)
	}

	if forceDateVersion {
		now := time.Now().UTC()
		dateVersion := fmt.Sprintf("v0.0.0-%s", now.Format("20060102150405"))
		version := &vos.Version{
			Raw: dateVersion,
		}
		return version, lastCommit, nil
	}

	lastTag, err := vc.gitRepo.GetLastSemverTag(ctx, repoPath)
	if err != nil {
		// Asumimos que es el primer release si no hay tags
		lastTag = ""
	}

	currentVersion := vc.parseVersionFromTag(lastTag)

	commits, err := vc.gitRepo.GetCommitsSinceTag(ctx, repoPath, lastTag)
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudieron obtener los commits desde el último tag: %w", err)
	}

	nextVersion := vc.calculateIncrement(currentVersion, commits)

	return &nextVersion, lastCommit, nil
}

func (vc *VersionCalculatorService) parseVersionFromTag(tag string) vos.Version {
	matches := semverTagRegex.FindStringSubmatch(tag)
	if len(matches) != 4 {
		return vos.Version{Major: 0, Minor: 0, Patch: 0, Raw: "v0.0.0"} // Versión inicial
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return vos.Version{Major: major, Minor: minor, Patch: patch, Raw: tag}
}

func (vc *VersionCalculatorService) calculateIncrement(current vos.Version, commits []*vos.Commit) vos.Version {
	highestChange := changeNone

	for _, commit := range commits {
		if strings.Contains(commit.Message, "BREAKING CHANGE") {
			highestChange = changeMajor
			break // Un cambio mayor tiene la máxima precedencia, no necesitamos seguir buscando.
		}

		matches := conventionalCommitRegex.FindStringSubmatch(commit.Message)
		if len(matches) > 2 {
			commitType := matches[1]
			breakingExclamation := matches[3]

			if breakingExclamation == "!" {
				highestChange = changeMajor
				break // Un cambio mayor tiene la máxima precedencia.
			}

			switch commitType {
			case "feat":
				if highestChange < changeMinor {
					highestChange = changeMinor
				}
			case "fix":
				if highestChange < changePatch {
					highestChange = changePatch
				}
			}
		}
	}

	next := current
	if highestChange == changeNone {
		// Si no hubo cambios, devolvemos la versión actual sin modificar.
		return next
	}

	switch highestChange {
	case changeMajor:
		next.Major++
		next.Minor = 0
		next.Patch = 0
	case changeMinor:
		next.Minor++
		next.Patch = 0
	case changePatch:
		next.Patch++
	}

	next.Raw = fmt.Sprintf("v%d.%d.%d", next.Major, next.Minor, next.Patch)
	return next
}
