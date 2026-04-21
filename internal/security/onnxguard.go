//go:build ORT

package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knights-analytics/hugot/pipelines"

	"github.com/fullsend-ai/fullsend/internal/sentencetoken"
)

// ONNXGuardScanner runs the ProtectAI DeBERTa-v3 ONNX model for ML-based
// prompt injection detection using the hugot Go ONNX runtime — no Python
// subprocess. Replaces the former Python-based LLMGuardScanner.
type ONNXGuardScanner struct {
	pipeline  *pipelines.TextClassificationPipeline
	threshold float64
	matchType string
}

// NewONNXGuardScanner creates a scanner with the given pipeline, threshold,
// and match type. The pipeline must be pre-initialized from a hugot session.
// modelPath is used only for label validation at construction time.
func NewONNXGuardScanner(pipeline *pipelines.TextClassificationPipeline, modelPath string, threshold float64, matchType string) (*ONNXGuardScanner, error) {
	if threshold == 0 {
		threshold = 0.92
	}
	if matchType == "" {
		matchType = "sentence"
	}

	if err := validateLabels(modelPath); err != nil {
		return nil, fmt.Errorf("onnxguard: %w", err)
	}

	return &ONNXGuardScanner{
		pipeline:  pipeline,
		threshold: threshold,
		matchType: matchType,
	}, nil
}

func validateLabels(modelPath string) error {
	cfgPath := filepath.Join(modelPath, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("reading config.json: %w", err)
	}

	var cfg struct {
		ID2Label map[string]string `json:"id2label"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing config.json: %w", err)
	}

	if cfg.ID2Label["1"] != "INJECTION" {
		return fmt.Errorf("label ordering mismatch: id2label=%v (expected id2label[\"1\"]=INJECTION)", cfg.ID2Label)
	}
	return nil
}

// Scan runs the ProtectAI DeBERTa-v3 prompt injection scanner on the
// given text. In sentence mode, splits text into sentences and takes
// the max injection score.
func (s *ONNXGuardScanner) Scan(text string) ScanResult {
	if strings.TrimSpace(text) == "" {
		return ScanResult{Safe: true}
	}

	ctx := context.Background()

	var maxScore float64
	var err error

	if s.matchType == "sentence" {
		sents := sentencetoken.SplitSentences(text)
		sents = splitLongSentences(sents)
		maxScore, err = s.maxSentenceScore(ctx, sents)
	} else {
		maxScore, err = s.scoreText(ctx, text)
	}

	if err != nil {
		return ScanResult{
			Safe: false,
			Findings: []Finding{{
				Scanner:  "llm_guard",
				Name:     "scanner_error",
				Severity: "high",
				Detail:   fmt.Sprintf("ONNX scanner error (fail-closed): %v", err),
				Position: -1,
			}},
		}
	}

	if maxScore >= s.threshold {
		return ScanResult{
			Safe: false,
			Findings: []Finding{{
				Scanner:  "llm_guard",
				Name:     "prompt_injection_ml",
				Severity: "critical",
				Detail:   fmt.Sprintf("DeBERTa-v3 detected injection (risk_score=%.3f, threshold=%.3f)", maxScore, s.threshold),
				Position: -1,
			}},
		}
	}

	return ScanResult{Safe: true}
}

func (s *ONNXGuardScanner) scoreText(ctx context.Context, text string) (float64, error) {
	result, err := s.pipeline.RunPipeline(ctx, []string{text})
	if err != nil {
		return 0, err
	}

	for _, o := range result.ClassificationOutputs[0] {
		if o.Label == "INJECTION" {
			return float64(o.Score), nil
		}
	}
	return 0, nil
}

const maxSentenceChars = 1500

func splitLongSentences(sents []string) []string {
	result := make([]string, 0, len(sents))
	for _, s := range sents {
		if len(s) <= maxSentenceChars {
			result = append(result, s)
			continue
		}
		stride := maxSentenceChars / 2
		for start := 0; start < len(s); start += stride {
			end := start + maxSentenceChars
			if end >= len(s) {
				result = append(result, s[start:])
				break
			}
			if cut := strings.LastIndexByte(s[start:end], ' '); cut > 0 {
				end = start + cut
			}
			result = append(result, s[start:end])
		}
	}
	return result
}

const maxSentenceBatch = 200

func (s *ONNXGuardScanner) maxSentenceScore(ctx context.Context, sents []string) (float64, error) {
	if len(sents) == 0 {
		return 0, nil
	}

	var max float64
	for i := 0; i < len(sents); i += maxSentenceBatch {
		end := i + maxSentenceBatch
		if end > len(sents) {
			end = len(sents)
		}
		chunk := sents[i:end]

		result, err := s.pipeline.RunPipeline(ctx, chunk)
		if err != nil {
			return 0, err
		}

		for _, outputs := range result.ClassificationOutputs {
			for _, o := range outputs {
				if o.Label == "INJECTION" && float64(o.Score) > max {
					max = float64(o.Score)
				}
			}
		}

		if max >= s.threshold {
			return max, nil
		}
	}
	return max, nil
}

// Name returns the scanner identifier. Preserves "llm_guard" for backward
// compatibility with log parsers and dashboards.
func (s *ONNXGuardScanner) Name() string {
	return "llm_guard"
}
