package aggregates_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex-client/internal/domain/architecture/aggregates"
	"github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"
)

func TestNewQuestion(t *testing.T) {
	t.Run("should create a question with valid points", func(t *testing.T) {
		// Arrange
		points := []vos.Point{
			vos.NewPoint(1, 0.2, "Low", "Low"),
			vos.NewPoint(2, 0.5, "Medium", "Medium"),
			vos.NewPoint(3, 0.8, "High", "High"),
		}

		questionString := "¿Cuál es tu prioridad?"
		// Act
		question, err := aggregates.NewQuestion(questionString, points)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, questionString, question.Ask())
		assert.Len(t, question.Points(), 3)
	})

	t.Run("should create a question with empty points", func(t *testing.T) {
		// Arrange
		points := []vos.Point{}

		questionString := "¿Cuál es tu presupuesto?"
		// Act
		question, err := aggregates.NewQuestion(questionString, points)

		// Assert
		require.Error(t, err)
		assert.Nil(t, question)
	})

	t.Run("should create a question with nil points", func(t *testing.T) {
		// Act
		question, err := aggregates.NewQuestion("¿Alguna pregunta?", nil)

		// Assert
		require.Error(t, err)
		assert.Nil(t, question)
	})

	t.Run("should return error when points have duplicate values", func(t *testing.T) {
		// Arrange
		points := []vos.Point{
			vos.NewPoint(1, 0.3, "Opción A", "Opción A"),
			vos.NewPoint(1, 0.7, "Opción B", "Opción B"),
		}

		// Act
		question, err := aggregates.NewQuestion("¿Duplicados?", points)

		// Assert
		require.Error(t, err)
		assert.Nil(t, question)
	})

	t.Run("should return error when multiple points share the same value", func(t *testing.T) {
		// Arrange
		points := []vos.Point{
			vos.NewPoint(1, 0.2, "Bajo", "Bajo"),
			vos.NewPoint(2, 0.5, "Medio", "Medio"),
			vos.NewPoint(2, 0.8, "Alto", "Alto"),
		}

		// Act
		question, err := aggregates.NewQuestion("¿Tres con duplicado?", points)

		// Assert
		require.Error(t, err)
		assert.Nil(t, question)
	})

	t.Run("should allow a single point", func(t *testing.T) {
		// Arrange
		points := []vos.Point{
			vos.NewPoint(1, 1.0, "Única opción", "Única opción"),
		}

		// Act
		q, err := aggregates.NewQuestion("¿Solo una?", points)

		// Assert
		require.NoError(t, err)
		assert.Len(t, q.Points(), 1)
	})
}

func TestQuestion_Points(t *testing.T) {
	t.Run("should return the same points passed at creation", func(t *testing.T) {
		// Arrange
		point1Value := 1
		point1Description := "Bajo"
		point1Weight := 0.2

		point2Value := 2
		point2Description := "Alto"
		point2Weight := 0.8

		points := []vos.Point{
			vos.NewPoint(point1Value, point1Weight, point1Description, point1Description),
			vos.NewPoint(point2Value, point2Weight, point2Description, point2Description),
		}
		q, err := aggregates.NewQuestion("¿Prioridad?", points)
		require.NoError(t, err)

		// Act
		result := q.Points()

		// Assert
		assert.Len(t, result, 2)
		assert.Equal(t, point1Value, result[0].Value())
		assert.Equal(t, point1Description, result[0].Description())
		assert.Equal(t, point1Weight, result[0].Weight())
		assert.Equal(t, point2Value, result[1].Value())
		assert.Equal(t, point2Description, result[1].Description())
		assert.Equal(t, point2Weight, result[1].Weight())
	})
}
