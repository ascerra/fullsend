package sticky

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fullsend-ai/fullsend/internal/forge"
	"github.com/fullsend-ai/fullsend/internal/ui"
)

const defaultMaxSize = 65000

// Config controls how a sticky comment is identified and managed.
type Config struct {
	Marker       string // hidden HTML comment, e.g. "<!-- fullsend:review-agent -->"
	FooterMarker string // optional footer delimiter, stripped before collapsing history
	MaxSize      int    // max comment body size (default 65000)
	DryRun       bool
}

func (c Config) maxSize() int {
	if c.MaxSize > 0 {
		return c.MaxSize
	}
	return defaultMaxSize
}

// Post implements the sticky comment lifecycle: find an existing comment
// bearing the marker, collapse old content into history, and create or
// update in-place.
func Post(ctx context.Context, client forge.Client, owner, repo string, number int, body string, cfg Config, printer *ui.Printer) error {
	comments, err := client.ListIssueComments(ctx, owner, repo, number)
	if err != nil {
		return fmt.Errorf("listing comments: %w", err)
	}

	existing := FindMarkedComment(comments, cfg.Marker)
	markedBody := cfg.Marker + "\n" + body

	if existing != nil {
		printer.StepStart("Found existing comment, updating in-place")

		newBody := BuildUpdatedBody(existing.Body, markedBody, cfg)

		if cfg.DryRun {
			printer.StepInfo("Dry run — would update comment " + strconv.Itoa(existing.ID))
			printer.StepInfo("Body length: " + strconv.Itoa(len(newBody)))
			return nil
		}

		if err := client.UpdateIssueComment(ctx, owner, repo, existing.ID, newBody); err != nil {
			return fmt.Errorf("updating comment: %w", err)
		}
		printer.StepDone("Comment updated")
	} else {
		printer.StepStart("No existing comment found, creating new one")

		if cfg.DryRun {
			printer.StepInfo("Dry run — would create new comment")
			printer.StepInfo("Body length: " + strconv.Itoa(len(markedBody)))
			return nil
		}

		if _, err := client.CreateIssueComment(ctx, owner, repo, number, markedBody); err != nil {
			return fmt.Errorf("creating comment: %w", err)
		}
		printer.StepDone("Comment created")
	}

	return nil
}

// FindMarkedComment returns the first comment whose body contains the
// given marker string, or nil if none is found.
func FindMarkedComment(comments []forge.IssueComment, marker string) *forge.IssueComment {
	for i := range comments {
		if strings.Contains(comments[i].Body, marker) {
			return &comments[i]
		}
	}
	return nil
}

// detailsRe matches a <details>...<summary>Previous ...</summary>...</details> block.
var detailsRe = regexp.MustCompile(`(?s)<details>\s*<summary>Previous [^<]*</summary>\s*(.*?)\s*</details>`)

// BuildUpdatedBody collapses the old comment body into a flat list of
// <details> blocks and prepends the new body. Footer content (delimited
// by FooterMarker) is stripped before collapsing and re-appended after.
func BuildUpdatedBody(oldBody, newBody string, cfg Config) string {
	// Strip marker from the old body.
	oldContent := strings.Replace(oldBody, cfg.Marker+"\n", "", 1)
	oldContent = strings.Replace(oldContent, cfg.Marker, "", 1)

	// Strip footer if configured.
	var footer string
	if cfg.FooterMarker != "" {
		if idx := strings.Index(oldContent, cfg.FooterMarker); idx >= 0 {
			footer = oldContent[idx:]
			oldContent = strings.TrimRight(oldContent[:idx], "\n")
		}
	}

	// Extract existing <details> blocks from old content to flatten history.
	var historyBlocks []string
	matches := detailsRe.FindAllStringSubmatch(oldContent, -1)
	for _, m := range matches {
		historyBlocks = append(historyBlocks, m[1])
	}

	// The "current" old content is everything minus the old <details> blocks.
	currentOld := detailsRe.ReplaceAllString(oldContent, "")
	currentOld = strings.TrimSpace(currentOld)

	// Build flat history: the old current content becomes the newest history
	// entry, followed by any previously accumulated history entries.
	var collapsed strings.Builder
	if currentOld != "" {
		collapsed.WriteString("\n\n<details>\n<summary>Previous run</summary>\n\n")
		collapsed.WriteString(currentOld)
		collapsed.WriteString("\n\n</details>")
	}
	for i, block := range historyBlocks {
		collapsed.WriteString(fmt.Sprintf("\n\n<details>\n<summary>Previous run (%d)</summary>\n\n", i+2))
		collapsed.WriteString(strings.TrimSpace(block))
		collapsed.WriteString("\n\n</details>")
	}

	combined := newBody + collapsed.String()

	// Re-append footer.
	if footer != "" {
		combined += "\n\n" + footer
	}

	if len(combined) > cfg.maxSize() {
		combined = TruncateBody(combined, cfg.maxSize())
	}

	return combined
}

// TruncateBody trims body to fit within maxSize, keeping the current
// content at the top and trimming history from the end. The truncation
// point is aligned to a valid UTF-8 boundary.
func TruncateBody(body string, maxSize int) string {
	if len(body) <= maxSize {
		return body
	}

	truncationMsg := "\n\n---\n*Previous history truncated due to comment size limits.*"
	budget := maxSize - len(truncationMsg)
	if budget < 0 {
		budget = 0
	}

	// Walk backward to a valid UTF-8 boundary.
	for budget > 0 && !utf8.RuneStart(body[budget]) {
		budget--
	}

	return body[:budget] + truncationMsg
}
