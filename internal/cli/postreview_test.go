package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseReviewResult_JSON(t *testing.T) {
	input := `{"body": "Looks good!", "action": "approve"}`
	result := parseReviewResult(input)
	assert.Equal(t, "Looks good!", result.Body)
	assert.Equal(t, "approve", result.Action)
}

func TestParseReviewResult_PlainText(t *testing.T) {
	input := "This is plain text review."
	result := parseReviewResult(input)
	assert.Equal(t, input, result.Body)
	assert.Equal(t, "comment", result.Action)
}

func TestParseReviewResult_DefaultAction(t *testing.T) {
	input := `{"body": "Some review"}`
	result := parseReviewResult(input)
	assert.Equal(t, "Some review", result.Body)
	assert.Equal(t, "comment", result.Action)
}

func TestParseReviewResult_EmptyBody(t *testing.T) {
	input := `{"action": "approve"}`
	result := parseReviewResult(input)
	assert.Equal(t, input, result.Body)
	assert.Equal(t, "approve", result.Action)
}
