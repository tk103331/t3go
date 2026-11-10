package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/upstream"
)

// upstreamReviewCommit sends the branch commit list the review panel shows.
func (s *Server) upstreamReviewCommit(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		Cwd     string `json:"cwd"`
		BaseRef string `json:"baseRef"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid commit list request"))
		return true
	}
	if !s.allowedDir(input.Cwd) {
		_ = writer.write(upstream.UnknownFailure(requestID, "unknown working directory"))
		return true
	}
	commits, truncated, err := git.BranchCommits(ctx, input.Cwd, input.BaseRef, 100)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
		return true
	}
	items := make([]any, 0, len(commits))
	for _, commit := range commits {
		items = append(items, map[string]any{
			"sha": commit.SHA, "subject": commit.Subject, "committedAt": commit.CommittedAt,
		})
	}
	return writer.write(upstream.Exit(requestID, map[string]any{
		"commits": items, "truncated": truncated,
	})) == nil
}

// upstreamReviewDiff sends the diff sources the review panel renders. It
// answers metadata-only requests with file stats alone, as the panel then
// reads each file's contents separately.
func (s *Server) upstreamReviewDiff(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		Cwd              string `json:"cwd"`
		BaseRef          string `json:"baseRef"`
		CommitSha        string `json:"commitSha"`
		IgnoreWhitespace bool   `json:"ignoreWhitespace"`
		SourceKind       string `json:"sourceKind"`
		TrackedOnly      bool   `json:"trackedOnly"`
		MetadataOnly     bool   `json:"metadataOnly"`
		File             *struct {
			Path         string  `json:"path"`
			PreviousPath *string `json:"previousPath"`
			SourceKind   string  `json:"sourceKind"`
		} `json:"file"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid diff request"))
		return true
	}
	if !s.allowedDir(input.Cwd) {
		_ = writer.write(upstream.UnknownFailure(requestID, "unknown working directory"))
		return true
	}
	kind := input.SourceKind
	if input.File != nil && input.File.SourceKind != "" {
		kind = input.File.SourceKind
	}
	if input.CommitSha != "" {
		kind = "commit"
	}
	if kind == "" {
		kind = "working-tree"
	}
	var paths []string
	if input.File != nil {
		paths = append(paths, input.File.Path)
		if input.File.PreviousPath != nil && *input.File.PreviousPath != "" {
			paths = append(paths, *input.File.PreviousPath)
		}
	}
	var from, to, id, title, commitSubject, baseRef, headRef string
	switch kind {
	case "commit":
		subject, parent, err := git.CommitMeta(ctx, input.Cwd, input.CommitSha)
		if err != nil {
			_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
			return true
		}
		from, to = parent, input.CommitSha
		if from == "" {
			from = git.EmptyTree
		}
		id = input.CommitSha
		title = "Commit " + shortHash(input.CommitSha)
		commitSubject = subject
		baseRef, headRef = parent, input.CommitSha
	case "branch-range":
		base := git.ReviewBase(ctx, input.Cwd, input.BaseRef)
		from, to = base, "HEAD"
		id = "branch-range"
		title = "Branch changes"
		baseRef, headRef = base, git.Status(ctx, input.Cwd).Branch
	default:
		from, to = "HEAD", ""
		id = "working-tree"
		title = "Working tree"
		baseRef, headRef = input.BaseRef, git.Status(ctx, input.Cwd).Branch
	}
	withPatch := !input.MetadataOnly || input.File != nil
	diff, err := git.ReviewDiff(ctx, input.Cwd, from, to, paths, input.IgnoreWhitespace, input.TrackedOnly, withPatch)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
		return true
	}
	files := make([]any, 0, len(diff.Files))
	for _, file := range diff.Files {
		var previous any
		if file.PreviousPath != "" {
			previous = file.PreviousPath
		}
		files = append(files, map[string]any{
			"path": file.Path, "previousPath": previous,
			"additions": file.Additions, "deletions": file.Deletions,
		})
	}
	source := map[string]any{
		"id": id, "kind": kind, "title": title, "diff": diff.Patch,
		"diffHash": git.DiffHash(diff.Patch, diff.Files), "truncated": diff.Truncated,
		"files": files, "baseRef": nilStringOrValue(baseRef), "headRef": nilStringOrValue(headRef),
	}
	if commitSubject != "" {
		source["commitSubject"] = commitSubject
	}
	if input.MetadataOnly && input.File == nil {
		source["patchesDeferred"] = true
	}
	return writer.write(upstream.Exit(requestID, map[string]any{
		"cwd": input.Cwd, "generatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"sources": []any{source},
	})) == nil
}

// upstreamReviewFile sends one file's old and new contents.
func (s *Server) upstreamReviewFile(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		Cwd        string  `json:"cwd"`
		SourceKind string  `json:"sourceKind"`
		BaseRef    *string `json:"baseRef"`
		HeadRef    *string `json:"headRef"`
		OldPath    string  `json:"oldPath"`
		NewPath    string  `json:"newPath"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid file contents request"))
		return true
	}
	if !s.allowedDir(input.Cwd) {
		_ = writer.write(upstream.UnknownFailure(requestID, "unknown working directory"))
		return true
	}
	base, head := "", ""
	if input.BaseRef != nil {
		base = *input.BaseRef
	}
	if input.HeadRef != nil {
		head = *input.HeadRef
	}
	oldContents, newContents, err := git.FileContents(ctx, input.Cwd, input.SourceKind, base, head, input.OldPath, input.NewPath)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
		return true
	}
	return writer.write(upstream.Exit(requestID, map[string]any{
		"oldContents": oldContents, "newContents": newContents,
	})) == nil
}

func shortHash(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

func nilStringOrValue(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
