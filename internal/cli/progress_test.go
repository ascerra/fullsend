package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fullsend-ai/fullsend/internal/ui"
)

func TestExtractBinaryName(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"make test", "make"},
		{"git commit -s -m 'msg'", "git"},
		{"/usr/bin/make lint", "make"},
		{"  go test ./...", "go"},
		{"", ""},
		{"gh pr create --title 'x'", "gh"},
		{"curl -H 'Authorization: Bearer SECRET' https://api.example.com", "curl"},
	}
	for _, tt := range tests {
		got := extractBinaryName(tt.cmd)
		if got != tt.want {
			t.Errorf("extractBinaryName(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

func TestExtractSafeContext(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]interface{}
		want     string
	}{
		{
			name:     "bash command",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "make test"},
			want:     "make",
		},
		{
			name:     "bash with secret in args",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "curl -H 'Bearer token123' https://api.example.com"},
			want:     "curl",
		},
		{
			name:     "read file",
			toolName: "Read",
			input:    map[string]interface{}{"file_path": "/src/main.go"},
			want:     "/src/main.go",
		},
		{
			name:     "edit file",
			toolName: "Edit",
			input:    map[string]interface{}{"file_path": "/src/main.go", "old_string": "secret", "new_string": "redacted"},
			want:     "/src/main.go",
		},
		{
			name:     "grep pattern",
			toolName: "Grep",
			input:    map[string]interface{}{"pattern": "func main"},
			want:     "func main",
		},
		{
			name:     "unknown tool",
			toolName: "Agent",
			input:    map[string]interface{}{"prompt": "do something"},
			want:     "",
		},
		{
			name:     "empty input",
			toolName: "Bash",
			input:    map[string]interface{}{},
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.input)
			got := extractSafeContext(tt.toolName, raw)
			if got != tt.want {
				t.Errorf("extractSafeContext(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestProgressParser(t *testing.T) {
	// Build NDJSON with assistant-type tool_use messages.
	lines := []string{
		`{"type":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/src/main.go"}}]}`,
		`{"type":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"make test"}}]}`,
		`{"type":"assistant","content":[{"type":"text","text":"Done"}]}`,
		`{"type":"result","result":"All done"}`,
	}

	input := strings.NewReader(strings.Join(lines, "\n"))
	var buf bytes.Buffer
	printer := ui.New(&buf)
	start := time.Now()
	metrics := &RunMetrics{}

	progressParser(input, printer, start, metrics)

	if metrics.ToolCalls != 2 {
		t.Errorf("expected 2 tool calls, got %d", metrics.ToolCalls)
	}

	output := buf.String()
	if !strings.Contains(output, "Read: /src/main.go") {
		t.Errorf("expected Read progress, got: %s", output)
	}
	if !strings.Contains(output, "Bash: make") {
		t.Errorf("expected Bash progress, got: %s", output)
	}
}

func TestProgressParserStreamEvents(t *testing.T) {
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Edit"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop"}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Bash"}}}`,
	}

	input := strings.NewReader(strings.Join(lines, "\n"))
	var buf bytes.Buffer
	printer := ui.New(&buf)
	metrics := &RunMetrics{}

	progressParser(input, printer, time.Now(), metrics)

	if metrics.ToolCalls != 2 {
		t.Errorf("expected 2 tool calls from stream events, got %d", metrics.ToolCalls)
	}
}
