package vos_test

import (
	"fmt"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStepNameDefinition(t *testing.T) {
	testCases := []struct {
		name          string
		input         string
		expectedOrder int
		expectedName  string
		expectError   bool
	}{
		{
			name:          "should create a valid step name with single digit",
			input:         "01-test",
			expectedOrder: 1,
			expectedName:  "test",
			expectError:   false,
		},
		{
			name:          "should create a valid step name with double digits",
			input:         "10-deploy",
			expectedOrder: 10,
			expectedName:  "deploy",
			expectError:   false,
		},
		{
			name:          "should create a valid step name with hyphens in name",
			input:         "02-integration-test",
			expectedOrder: 2,
			expectedName:  "integration-test",
			expectError:   false,
		},
		{
			name:        "should return error for format without hyphen",
			input:       "01test",
			expectError: true,
		},
		{
			name:        "should return error for format without number",
			input:       "test-deploy",
			expectError: true,
		},
		{
			name:        "should return error for format with non-numeric order",
			input:       "aa-test",
			expectError: true,
		},
		{
			name:        "should return error for empty name after hyphen",
			input:       "03-",
			expectError: true,
		},
		{
			name:        "should return error for empty input",
			input:       "",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stepName, err := vos.NewStepNameDefinition(tc.input)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedOrder, stepName.Order())
				assert.Equal(t, tc.expectedName, stepName.Name())
			}
		})
	}
}

func TestStepNameDefinition_FullName(t *testing.T) {
	testCases := []struct {
		input          string
		expectedOutput string
	}{
		{"01-test", "01-test"},
		{"12-deploy", "12-deploy"},
		{"05-another-step", "05-another-step"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("should reconstruct %s", tc.input), func(t *testing.T) {
			stepName, err := vos.NewStepNameDefinition(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedOutput, stepName.FullName())
		})
	}
}
