package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fullsend-ai/fullsend/internal/ui"
)

// streamEvent represents a single NDJSON event from Claude Code's stream-json output.
type streamEvent struct {
	Type  string `json:"type"`
	Event *struct {
		Type         string       `json:"type"`
		ContentBlock contentBlock `json:"content_block"`
	} `json:"event,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// toolResult is emitted when a tool completes.
type toolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
}

// assistantMessage contains tool_use blocks from complete assistant messages.
type assistantMessage struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

type contentItem struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// RunMetrics collects execution statistics from stream parsing.
type RunMetrics struct {
	ToolCalls int `json:"tool_calls"`
}

// progressParser reads NDJSON from Claude Code's stream-json output and emits
// progress updates via the printer. It extracts tool names and safe context
// (binary name for Bash, file path for Read/Write/Edit) without logging
// potentially sensitive arguments.
func progressParser(r io.Reader, printer *ui.Printer, start time.Time, metrics *RunMetrics) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	isCI := os.Getenv("GITHUB_ACTIONS") == "true"

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var evt streamEvent
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}

		switch {
		case evt.Type == "assistant":
			parseAssistantToolUse(line, printer, start, metrics, isCI)

		case evt.Type == "stream_event" && evt.Event != nil:
			if evt.Event.Type == "content_block_start" && evt.Event.ContentBlock.Type == "tool_use" {
				toolName := evt.Event.ContentBlock.Name
				metrics.ToolCalls++
				emitToolProgress(printer, toolName, "", start, metrics.ToolCalls, isCI)
			}
		}
	}
}

func parseAssistantToolUse(line []byte, printer *ui.Printer, start time.Time, metrics *RunMetrics, isCI bool) {
	var msg assistantMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return
	}

	var items []contentItem
	if err := json.Unmarshal(msg.Content, &items); err != nil {
		return
	}

	for _, item := range items {
		if item.Type != "tool_use" {
			continue
		}
		metrics.ToolCalls++
		context := extractSafeContext(item.Name, item.Input)
		emitToolProgress(printer, item.Name, context, start, metrics.ToolCalls, isCI)
	}
}

// extractSafeContext returns a safe, non-secret string for progress display.
func extractSafeContext(toolName string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return ""
	}

	switch toolName {
	case "Bash":
		raw, ok := fields["command"]
		if !ok {
			return ""
		}
		var cmd string
		if err := json.Unmarshal(raw, &cmd); err != nil {
			return ""
		}
		return extractBinaryName(cmd)

	case "Read", "Write", "Edit":
		raw, ok := fields["file_path"]
		if !ok {
			return ""
		}
		var path string
		if err := json.Unmarshal(raw, &path); err != nil {
			return ""
		}
		return path

	case "Grep":
		raw, ok := fields["pattern"]
		if !ok {
			return ""
		}
		var pattern string
		if err := json.Unmarshal(raw, &pattern); err != nil {
			return ""
		}
		return pattern

	case "Glob":
		raw, ok := fields["pattern"]
		if !ok {
			return ""
		}
		var pattern string
		if err := json.Unmarshal(raw, &pattern); err != nil {
			return ""
		}
		return pattern
	}

	return ""
}

// extractBinaryName returns only the first token of a shell command.
func extractBinaryName(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	firstWord := strings.Fields(cmd)[0]
	// Strip path prefix if present (e.g. /usr/bin/make → make).
	if idx := strings.LastIndex(firstWord, "/"); idx >= 0 {
		firstWord = firstWord[idx+1:]
	}
	return firstWord
}

func emitToolProgress(printer *ui.Printer, toolName, context string, start time.Time, toolCount int, isCI bool) {
	elapsed := time.Since(start).Truncate(time.Second)

	var msg string
	if context != "" {
		msg = fmt.Sprintf("%s: %s (%s, %d tools)", toolName, context, elapsed, toolCount)
	} else {
		msg = fmt.Sprintf("%s (%s, %d tools)", toolName, elapsed, toolCount)
	}

	if isCI {
		fmt.Fprintf(os.Stderr, "::notice::%s\n", msg)
	}
	printer.Heartbeat(msg)
}
