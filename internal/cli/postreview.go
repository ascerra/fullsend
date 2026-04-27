package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	gh "github.com/fullsend-ai/fullsend/internal/forge/github"
	"github.com/fullsend-ai/fullsend/internal/sticky"
	"github.com/fullsend-ai/fullsend/internal/ui"
)

const reviewMarker = "<!-- fullsend:review-agent -->"

func newPostReviewCmd() *cobra.Command {
	var (
		repo   string
		pr     int
		result string
		token  string
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "post-review",
		Short: "Post or update a sticky review comment on a PR",
		Long: `Posts review findings as a sticky issue comment on a pull request.

On first run, creates a new comment with a hidden HTML marker.
On re-runs, finds the existing comment, collapses old content into
a <details> block, and edits in-place. This prevents review comment
flooding on force-push, manual re-run, or workflow retry.

The --result flag accepts a file path containing the review body text,
or reads from stdin if set to "-".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := ui.New(os.Stdout)

			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			if token == "" {
				return fmt.Errorf("--token or GITHUB_TOKEN required")
			}

			parts := strings.SplitN(repo, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("--repo must be in owner/repo format, got %q", repo)
			}
			owner, repoName := parts[0], parts[1]

			raw, err := readReviewBody(result)
			if err != nil {
				return fmt.Errorf("reading review body: %w", err)
			}

			parsed := parseReviewResult(raw)

			printer.Header("Post Review")

			client := gh.New(token)
			cfg := sticky.Config{
				Marker: reviewMarker,
				DryRun: dryRun,
			}
			return sticky.Post(cmd.Context(), client, owner, repoName, pr, parsed.Body, cfg, printer)
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "repository in owner/repo format (required)")
	cmd.Flags().IntVar(&pr, "pr", 0, "pull request number (required)")
	cmd.Flags().StringVar(&result, "result", "-", "path to review body file, or '-' for stdin")
	cmd.Flags().StringVar(&token, "token", "", "GitHub token (default: $GITHUB_TOKEN)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be posted without making API calls")
	_ = cmd.MarkFlagRequired("repo")
	_ = cmd.MarkFlagRequired("pr")

	return cmd
}

// readReviewBody reads the review body from a file path or stdin.
func readReviewBody(path string) (string, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReviewResult represents a parsed review result file.
type ReviewResult struct {
	Body   string `json:"body"`
	Action string `json:"action"` // "approve", "request-changes", "comment"
}

// parseReviewResult attempts to parse the body as a JSON ReviewResult.
// If parsing fails, treats the entire input as a plain-text body.
func parseReviewResult(input string) ReviewResult {
	var result ReviewResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		return ReviewResult{Body: input, Action: "comment"}
	}
	if result.Body == "" {
		result.Body = input
	}
	if result.Action == "" {
		result.Action = "comment"
	}
	return result
}
