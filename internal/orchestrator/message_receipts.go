package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/store"
)

var (
	ErrInvalidRequestID = errors.New("request ID must contain 1-128 letters, digits, dots, dashes or underscores")
	ErrRequestConflict  = errors.New("request ID was already used for different message content or attachments")
	ErrAcceptedRunGone  = errors.New("request was already accepted, but its run was removed from history")
)

func ValidRequestID(value string) bool {
	if len(value) == 0 || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.') {
			return false
		}
	}
	return true
}

func messageHash(prompt string, attachments []string) string {
	// Empty and omitted attachment lists describe the same request.
	raw, _ := json.Marshal(struct {
		Content     string
		Attachments []string
	}{prompt, append([]string(nil), attachments...)})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (s *Service) acceptedRequest(ctx context.Context, threadID, requestID, hash string) (proto.Run, bool, error) {
	if requestID == "" {
		return proto.Run{}, false, nil
	}
	receipt, acceptedHash, err := s.Store.MessageReceipt(ctx, threadID, requestID)
	if errors.Is(err, store.ErrNotFound) {
		return proto.Run{}, false, nil
	}
	if err != nil {
		return proto.Run{}, false, err
	}
	if acceptedHash != hash {
		return proto.Run{}, true, ErrRequestConflict
	}
	run, err := s.Store.Run(ctx, receipt.RunID)
	if errors.Is(err, store.ErrNotFound) {
		err = ErrAcceptedRunGone
	}
	return run, true, err
}
