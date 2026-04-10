package secretarial

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Config holds runtime configuration injected from environment variables.
type Config struct {
	Repo        string // owner/name
	SearchQuery string // name-contains query for Shared Drive docs (e.g. "team sync")
	NameFilter      string // if set, only process docs whose name contains this substring (e.g. "Notes by Gemini")
	CredentialsJSON []byte // GCP service-account key (never on disk)
	GCPProjectID    string // GCP project for Vertex AI (enables LLM extraction when set)
	VertexRegion    string // Vertex AI region (default "us-east5")
	LLMModel        string // Claude model ID (default "claude-sonnet-4-6")
	LookbackHours   int
	IssueLimit      int  // max open issues to fetch for LLM context (default 500)
	DryRun          bool
	CommentsOnly    bool // only comment on existing issues; skip new issue creation
	Verbose         bool // print full topic detail — local use only, never in CI
}

// Run is the top-level orchestrator.
//
// Architecture: the LLM is untrusted. Its output passes through a deterministic
// security gate (ValidateForPublishing) before any GitHub write. The gate
// REJECTS actions that contain sensitive content — it never scrubs-and-posts.
//
// IMPORTANT: This runs in a public GitHub Actions log. Never log meeting
// content, doc names, topic titles, summaries, or anything derived from
// meeting notes. Only log aggregate counts and pass/fail status.
func Run(ctx context.Context, cfg Config) error {
	slog.Info("secretarial agent starting",
		"dry_run", cfg.DryRun,
		"comments_only", cfg.CommentsOnly,
		"lookback_hours", cfg.LookbackHours,
	)

	driveClient, err := NewDriveClient(ctx, cfg.CredentialsJSON)
	if err != nil {
		return fmt.Errorf("initializing drive client: %w", err)
	}

	cutoff := time.Now().UTC().Add(-time.Duration(cfg.LookbackHours) * time.Hour)

	docs, err := driveClient.SearchRecentDocs(ctx, cfg.SearchQuery, cutoff)
	if err != nil {
		return fmt.Errorf("finding meeting docs: %w", err)
	}
	slog.Info("drive search returned", "count", len(docs))
	for i, d := range docs {
		slog.Info("  doc",
			"index", i,
			"name", d.Name,
			"created", d.CreatedTime.Format("2006-01-02 15:04:05 UTC"),
			"modified", d.ModifiedTime.Format("2006-01-02 15:04:05 UTC"),
			"id", d.ID,
		)
	}

	if cfg.NameFilter != "" {
		filtered := docs[:0]
		for _, d := range docs {
			if strings.Contains(d.Name, cfg.NameFilter) {
				filtered = append(filtered, d)
			} else {
				slog.Info("  skipped (name filter)", "name", d.Name, "filter", cfg.NameFilter)
			}
		}
		docs = filtered
		slog.Info("after name filter", "kept", len(docs))
	}

	if len(docs) == 0 {
		slog.Info("no new meeting notes to process")
		return nil
	}

	var llm *LLMClient
	if cfg.GCPProjectID != "" {
		region := cfg.VertexRegion
		if region == "" {
			region = "us-east5"
		}
		model := cfg.LLMModel
		if model == "" {
			model = "claude-sonnet-4-6"
		}
		llm, err = NewLLMClient(ctx, cfg.CredentialsJSON, cfg.GCPProjectID, region, model)
		if err != nil {
			return fmt.Errorf("initializing LLM client: %w", err)
		}
		slog.Info("LLM extraction enabled", "model", model, "region", region)
	} else {
		slog.Info("LLM extraction disabled (no GCP_PROJECT_ID), using heuristic fallback")
	}

	issueLimit := cfg.IssueLimit
	if issueLimit <= 0 {
		issueLimit = 500
	}

	issueClient := NewIssueClient(cfg.Repo)
	openIssues, err := issueClient.ListOpen(issueLimit)
	if err != nil {
		return fmt.Errorf("listing open issues: %w", err)
	}
	slog.Info("loaded issue backlog", "count", len(openIssues), "limit", issueLimit)
	issueCtx := FormatIssueContext(openIssues)

	gate := DefaultGateConfig()

	var processed, failed int
	for _, doc := range docs {
		if err := processDoc(ctx, driveClient, issueClient, llm, issueCtx, doc, gate, cutoff, cfg.DryRun, cfg.CommentsOnly, cfg.Verbose); err != nil {
			slog.Error("error processing a doc", "err", err)
			failed++
		} else {
			processed++
		}
	}

	slog.Info("secretarial agent finished", "processed", processed, "failed", failed)
	return nil
}

// processDoc handles a single meeting doc.
//
// Flow: download → scrub input → extract topics (LLM or heuristic) →
// deduplicate → security gate (deterministic) → write to GitHub.
//
// The security gate is the hard boundary. Nothing the LLM produces reaches
// GitHub without passing ValidateForPublishing first.
func processDoc(
	ctx context.Context,
	dc *DriveClient,
	ic *IssueClient,
	llm *LLMClient,
	issueCtx string,
	doc DocMeta,
	gate GateConfig,
	cutoff time.Time,
	dryRun bool,
	commentsOnly bool,
	verbose bool,
) error {
	raw, err := dc.DownloadDocText(ctx, doc.ID)
	if err != nil {
		return fmt.Errorf("downloading doc: %w", err)
	}
	if len(raw) < 20 {
		slog.Info("skipping trivially short doc")
		return nil
	}

	cleaned := ScrubSensitiveContent(raw)
	notesURL := fmt.Sprintf("https://docs.google.com/document/d/%s/edit", doc.ID)

	cutoffDate := cutoff.Format("2006-01-02")

	var topics []Topic
	if llm != nil {
		topics, err = llm.ExtractTopics(ctx, cleaned, notesURL, issueCtx, cutoffDate)
		if err != nil {
			slog.Error("LLM extraction failed, falling back to heuristic", "err", err)
			topics = ExtractTopicsHeuristic(cleaned, notesURL)
		}
	} else {
		topics = ExtractTopicsHeuristic(cleaned, notesURL)
	}

	topics = deduplicateByIssue(topics)

	if verbose {
		slog.Info("--- doc detail (local only) ---", "name", doc.Name, "url", notesURL)
	}

	var commented, filed, skippedNewIssues, omitted, gateRejected, duplicateSkipped int
	for i, t := range topics {
		if t.OmitReason != nil {
			omitted++
			if verbose {
				slog.Info("  OMIT", "topic", t.Title, "reason", *t.OmitReason)
			}
			continue
		}

		// Gate-check the brief summary first (before expansion).
		if rejection := ValidateForPublishing(t, gate); rejection != nil {
			gateRejected++
			if verbose {
				slog.Info("  GATE REJECTED", "topic", t.Title, "reason", rejection.Reason)
			}
			continue
		}

		// Expand new issue bodies after the gate passes on the brief summary,
		// so we don't waste LLM calls on topics rejected for confidence/PII/etc.
		if t.NewIssueTitle != nil && llm != nil && !commentsOnly {
			expanded, err := llm.ExpandNewIssueBody(ctx, *t.NewIssueTitle, t.Summary, cleaned, notesURL, issueCtx)
			if err != nil {
				slog.Error("failed to expand new issue body, keeping brief summary", "topic", t.Title, "err", err)
			} else if len(expanded) > len(t.Summary) {
				topics[i].Summary = expanded
				t = topics[i]
				// Re-validate the expanded body (length, sensitive content).
				if rejection := ValidateForPublishing(t, gate); rejection != nil {
					gateRejected++
					if verbose {
						slog.Info("  GATE REJECTED (post-expand)", "topic", t.Title, "reason", rejection.Reason)
					}
					continue
				}
			}
		}

		if t.ExistingIssue != nil {
			if !dryRun {
				alreadyPosted, err := ic.HasCommentContaining(*t.ExistingIssue, notesURL)
				if err != nil {
					slog.Error("failed to check existing comments", "err", err)
				} else if alreadyPosted {
					duplicateSkipped++
					if verbose {
						slog.Info("  SKIP DUPLICATE", "issue", *t.ExistingIssue)
					}
					continue
				}
			}

			commented++
			if verbose {
				slog.Info("  COMMENT", "issue", *t.ExistingIssue, "topic", t.Title, "body", t.Summary)
			}
			if !dryRun {
				if err := ic.Comment(*t.ExistingIssue, t.Summary); err != nil {
					slog.Error("failed to comment on issue", "err", err)
				}
			}
		} else if t.NewIssueTitle != nil {
			if commentsOnly {
				skippedNewIssues++
				if verbose {
					slog.Info("  SKIP NEW ISSUE (comments-only mode)", "title", *t.NewIssueTitle, "body", t.Summary)
				}
				continue
			}
			filed++
			issueBody := formatNewIssueBody(t.Summary)
			if verbose {
				slog.Info("  NEW ISSUE", "title", *t.NewIssueTitle, "body", issueBody)
			}
			if !dryRun {
				if _, err := ic.Create(*t.NewIssueTitle, issueBody, []string{"meeting-notes", "triage"}); err != nil {
					slog.Error("failed to create issue", "err", err)
				}
			}
		}
	}

	logAttrs := []any{
		"topics_found", len(topics),
		"commented", commented,
		"filed", filed,
		"omitted", omitted,
		"gate_rejected", gateRejected,
		"dry_run", dryRun,
	}
	if skippedNewIssues > 0 {
		logAttrs = append(logAttrs, "skipped_new_issues", skippedNewIssues)
	}
	if duplicateSkipped > 0 {
		logAttrs = append(logAttrs, "duplicate_skipped", duplicateSkipped)
	}
	slog.Info("doc processed", logAttrs...)
	return nil
}

// deduplicateByIssue merges topics that reference the same existing issue
// into a single topic with a combined summary. This is a safety net for
// cases where the LLM produces multiple entries for the same issue despite
// being asked not to. New-issue and omitted topics pass through unchanged.
func deduplicateByIssue(topics []Topic) []Topic {
	type merged struct {
		topic     Topic
		summaries []string
	}

	seen := map[int]*merged{}
	var order []int
	var out []Topic

	for _, t := range topics {
		if t.ExistingIssue == nil {
			out = append(out, t)
			continue
		}
		issNum := *t.ExistingIssue
		if m, ok := seen[issNum]; ok {
			m.summaries = append(m.summaries, t.Summary)
			if t.Confidence > m.topic.Confidence {
				m.topic.Confidence = t.Confidence
				m.topic.Title = t.Title
			}
		} else {
			seen[issNum] = &merged{
				topic:     t,
				summaries: []string{t.Summary},
			}
			order = append(order, issNum)
		}
	}

	for _, issNum := range order {
		m := seen[issNum]
		if len(m.summaries) > 1 {
			m.topic.Summary = strings.Join(m.summaries, "\n\n")
		}
		out = append(out, m.topic)
	}

	return out
}

const newIssueBanner = `> [!NOTE]
> This issue was automatically generated from meeting notes by the secretarial agent.
> Please review, edit, and add any missing context before prioritizing.

`

func formatNewIssueBody(body string) string {
	return newIssueBanner + body
}
