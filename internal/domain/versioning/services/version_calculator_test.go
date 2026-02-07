package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/versioning/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGitRepository es una implementación de la interfaz GitRepository para pruebas.
type mockGitRepository struct {
	GetLastCommitFunc      func(ctx context.Context, repoPath string) (*vos.Commit, error)
	GetLastSemverTagFunc   func(ctx context.Context, repoPath string) (string, error)
	GetCommitsSinceTagFunc func(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error)
	CreateTagForCommitFunc func(ctx context.Context, repoPath string, commitHash string, tagName string) error
}

func (m *mockGitRepository) GetLastCommit(ctx context.Context, repoPath string) (*vos.Commit, error) {
	if m.GetLastCommitFunc != nil {
		return m.GetLastCommitFunc(ctx, repoPath)
	}
	return &vos.Commit{Hash: "dummyhash"}, nil
}

func (m *mockGitRepository) GetLastSemverTag(ctx context.Context, repoPath string) (string, error) {
	if m.GetLastSemverTagFunc != nil {
		return m.GetLastSemverTagFunc(ctx, repoPath)
	}
	return "", nil
}

func (m *mockGitRepository) GetCommitsSinceTag(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error) {
	if m.GetCommitsSinceTagFunc != nil {
		return m.GetCommitsSinceTagFunc(ctx, repoPath, tag)
	}
	return nil, nil
}

func (m *mockGitRepository) CreateTagForCommit(ctx context.Context, repoPath string, commitHash string, tagName string) error {
	if m.CreateTagForCommitFunc != nil {
		return m.CreateTagForCommitFunc(ctx, repoPath, commitHash, tagName)
	}
	return nil
}

func TestVersionCalculator_CalculateNextVersion(t *testing.T) {
	testCases := []struct {
		name               string
		repoMock           *mockGitRepository
		forceDateVersion   bool
		expectedVersion    string
		expectedCommitHash string
		expectErr          bool
	}{
		{
			name: "debería incrementar MAJOR si hay un BREAKING CHANGE",
			repoMock: &mockGitRepository{
				GetLastSemverTagFunc: func(ctx context.Context, repoPath string) (string, error) {
					return "v1.2.3", nil
				},
				GetCommitsSinceTagFunc: func(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error) {
					return []*vos.Commit{
						{Message: "feat: new feature\n\nBREAKING CHANGE: this breaks everything"},
						{Message: "fix: another bug"},
					}, nil
				},
			},
			expectedVersion: "v2.0.0",
		},
		{
			name: "debería incrementar MAJOR si un commit tiene '!'",
			repoMock: &mockGitRepository{
				GetLastSemverTagFunc: func(ctx context.Context, repoPath string) (string, error) {
					return "v1.2.3", nil
				},
				GetCommitsSinceTagFunc: func(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error) {
					return []*vos.Commit{
						{Message: "feat!: new api that is not backwards compatible"},
					}, nil
				},
			},
			expectedVersion: "v2.0.0",
		},
		{
			name: "debería incrementar MINOR si hay un 'feat' pero no breaking changes",
			repoMock: &mockGitRepository{
				GetLastSemverTagFunc: func(ctx context.Context, repoPath string) (string, error) {
					return "v1.2.3", nil
				},
				GetCommitsSinceTagFunc: func(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error) {
					return []*vos.Commit{
						{Message: "feat: add new button"},
						{Message: "fix: some bug"},
					}, nil
				},
			},
			expectedVersion: "v1.3.0",
		},
		{
			name: "debería incrementar PATCH si solo hay 'fix' commits",
			repoMock: &mockGitRepository{
				GetLastSemverTagFunc: func(ctx context.Context, repoPath string) (string, error) {
					return "v1.2.3", nil
				},
				GetCommitsSinceTagFunc: func(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error) {
					return []*vos.Commit{
						{Message: "fix: button color"},
						{Message: "docs: update readme"},
					}, nil
				},
			},
			expectedVersion: "v1.2.4",
		},
		{
			name: "no debería cambiar la versión si no hay commits 'feat' o 'fix'",
			repoMock: &mockGitRepository{
				GetLastSemverTagFunc: func(ctx context.Context, repoPath string) (string, error) {
					return "v1.2.3", nil
				},
				GetCommitsSinceTagFunc: func(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error) {
					return []*vos.Commit{
						{Message: "docs: explain api"},
						{Message: "chore: update dependencies"},
					}, nil
				},
			},
			expectedVersion: "v1.2.3", // La versión no cambia
		},
		{
			name: "debería empezar en v0.1.0 si no hay tags previos y hay un 'feat'",
			repoMock: &mockGitRepository{
				GetLastSemverTagFunc: func(ctx context.Context, repoPath string) (string, error) {
					return "", fmt.Errorf("no tags found") // Simula que no hay tags
				},
				GetCommitsSinceTagFunc: func(ctx context.Context, repoPath, tag string) ([]*vos.Commit, error) {
					return []*vos.Commit{
						{Message: "feat: initial feature"},
					}, nil
				},
			},
			expectedVersion: "v0.1.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			calculator := NewVersionCalculator(tc.repoMock)
			nextVersion, _, err := calculator.CalculateNextVersion(context.Background(), "/fake/repo", tc.forceDateVersion)

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedVersion, nextVersion.Raw)
			}
		})
	}
}
