package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/domain/architecture/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/architecture/services"
	"github.com/jairoprogramador/vex/internal/domain/architecture/vos"
)

func buildUniformQuestions(t *testing.T) []*aggregates.Question {
	t.Helper()
	points := []vos.Point{
		vos.NewPoint(1, 1.0, "Low", "Low"),
		vos.NewPoint(2, 1.0, "Medium", "Medium"),
		vos.NewPoint(3, 1.0, "High", "High"),
	}
	q1, err := aggregates.NewQuestion("Q1", points)
	require.NoError(t, err)
	q2, err := aggregates.NewQuestion("Q2", points)
	require.NoError(t, err)
	q3, err := aggregates.NewQuestion("Q3", points)
	require.NoError(t, err)
	return []*aggregates.Question{q1, q2, q3}
}

func buildWeightedQuestions(t *testing.T) []*aggregates.Question {
	t.Helper()
	points := []vos.Point{
		vos.NewPoint(1, 0.5, "Low", "Low"),
		vos.NewPoint(2, 1.0, "Medium", "Medium"),
		vos.NewPoint(3, 2.0, "High", "High"),
	}
	q1, err := aggregates.NewQuestion("Q1", points)
	require.NoError(t, err)
	q2, err := aggregates.NewQuestion("Q2", points)
	require.NoError(t, err)
	return []*aggregates.Question{q1, q2}
}

func buildLevels() []vos.Level {
	return []vos.Level{
		vos.NewLevel("Low", 0),
		vos.NewLevel("Medium", 1),
		vos.NewLevel("High", 2),
	}
}

// --- Score ---

func TestPunctuation_Score(t *testing.T) {
	questions := buildUniformQuestions(t)

	t.Run("should return minimum score for all-1 responses", func(t *testing.T) {
		// score = 1*1.0 + 1*1.0 + 1*1.0 = 3.0
		score, err := services.Score([]int{1, 1, 1}, questions)
		require.NoError(t, err)
		assert.Equal(t, 3.0, score)
	})

	t.Run("should return maximum score for all-3 responses", func(t *testing.T) {
		// score = 3*1.0 + 3*1.0 + 3*1.0 = 9.0
		score, err := services.Score([]int{3, 3, 3}, questions)
		require.NoError(t, err)
		assert.Equal(t, 9.0, score)
	})

	t.Run("should return correct score for mixed responses", func(t *testing.T) {
		// score = 1*1.0 + 2*1.0 + 3*1.0 = 6.0
		score, err := services.Score([]int{1, 2, 3}, questions)
		require.NoError(t, err)
		assert.Equal(t, 6.0, score)
	})

	t.Run("should return error when response count differs from questions", func(t *testing.T) {
		_, err := services.Score([]int{1, 2}, questions)
		require.Error(t, err)
	})

	t.Run("should return error when response value not found in points", func(t *testing.T) {
		_, err := services.Score([]int{1, 1, 99}, questions)
		require.Error(t, err)
		assert.Equal(t, "point not found", err.Error())
	})
}

func TestPunctuation_Score_WithWeights(t *testing.T) {
	// Points: (1, w=0.5), (2, w=1.0), (3, w=2.0) for each question
	questions := buildWeightedQuestions(t)

	t.Run("should apply correct weight for low response", func(t *testing.T) {
		// score = 1*0.5 + 1*0.5 = 1.0
		score, err := services.Score([]int{1, 1}, questions)
		require.NoError(t, err)
		assert.Equal(t, 1.0, score)
	})

	t.Run("should apply correct weight for high response", func(t *testing.T) {
		// score = 3*2.0 + 3*2.0 = 12.0
		score, err := services.Score([]int{3, 3}, questions)
		require.NoError(t, err)
		assert.Equal(t, 12.0, score)
	})

	t.Run("should apply different weights per response value", func(t *testing.T) {
		// score = 1*0.5 + 3*2.0 = 0.5 + 6.0 = 6.5
		score, err := services.Score([]int{1, 3}, questions)
		require.NoError(t, err)
		assert.Equal(t, 6.5, score)
	})
}

// --- ScoreMin ---

func TestPunctuation_ScoreMin(t *testing.T) {
	t.Run("should return min score with uniform weights", func(t *testing.T) {
		questions := buildUniformQuestions(t)
		// min = 1*1.0 + 1*1.0 + 1*1.0 = 3.0
		assert.Equal(t, 3.0, services.ScoreMin(questions))
	})

	t.Run("should return min score with varied weights", func(t *testing.T) {
		questions := buildWeightedQuestions(t)
		// min = 1*0.5 + 1*0.5 = 1.0
		assert.Equal(t, 1.0, services.ScoreMin(questions))
	})
}

// --- Level ---
// With 3 uniform questions (weight=1.0) and levels [0, 1, 2]:
//   scoreMin=3.0, scoreMax=9.0, coefficient=2.0
//   Low(0):    [3.0, 5.0)
//   Medium(1): [5.0, 7.0)
//   High(2):   [7.0, 10.0)

func TestPunctuation_Level(t *testing.T) {
	questions := buildUniformQuestions(t)
	levels := buildLevels()

	t.Run("should return Low for minimum score", func(t *testing.T) {
		level, err := services.Level(3.0, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "Low", level.Name())
		assert.Equal(t, 0, level.Value())
	})

	t.Run("should return Medium for mid-range score", func(t *testing.T) {
		level, err := services.Level(6.0, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "Medium", level.Name())
		assert.Equal(t, 1, level.Value())
	})

	t.Run("should return High for maximum score", func(t *testing.T) {
		level, err := services.Level(9.0, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "High", level.Name())
		assert.Equal(t, 2, level.Value())
	})

	t.Run("should return correct level at boundary between Low and Medium", func(t *testing.T) {
		// 5.0 is the start of Medium range [5.0, 7.0)
		level, err := services.Level(5.0, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "Medium", level.Name())
	})

	t.Run("should return correct level at boundary between Medium and High", func(t *testing.T) {
		// 7.0 is the start of High range [7.0, 10.0)
		level, err := services.Level(7.0, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "High", level.Name())
	})

	t.Run("should return Low for score just below Medium boundary", func(t *testing.T) {
		level, err := services.Level(4.99, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "Low", level.Name())
	})

	t.Run("should return error for score below minimum range", func(t *testing.T) {
		_, err := services.Level(1.0, levels, questions)
		require.Error(t, err)
		assert.Equal(t, "level not found", err.Error())
	})
}

// --- Score + Level integration ---

func TestPunctuation_ScoreToLevel(t *testing.T) {
	questions := buildUniformQuestions(t)
	levels := buildLevels()

	t.Run("all-low responses should yield Low level", func(t *testing.T) {
		score, err := services.Score([]int{1, 1, 1}, questions)
		require.NoError(t, err)
		level, err := services.Level(score, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "Low", level.Name())
	})

	t.Run("all-mid responses should yield Medium level", func(t *testing.T) {
		score, err := services.Score([]int{2, 2, 2}, questions)
		require.NoError(t, err)
		level, err := services.Level(score, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "Medium", level.Name())
	})

	t.Run("all-high responses should yield High level", func(t *testing.T) {
		score, err := services.Score([]int{3, 3, 3}, questions)
		require.NoError(t, err)
		level, err := services.Level(score, levels, questions)
		require.NoError(t, err)
		assert.Equal(t, "High", level.Name())
	})
}
