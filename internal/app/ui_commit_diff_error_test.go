package app

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
)

// An environment too old for the commit-diff endpoint says so, instead
// of showing the server's bare 404.
func TestUICommitDiffErrorExplainsOldEnvironment(t *testing.T) {
	message := patchDiffError(&client.APIError{Status: http.StatusNotFound, Message: "404 page not found"})
	if !strings.Contains(message, i18n.T("Update T3Go on the environment")) {
		t.Fatalf("404 was not explained: %q", message)
	}
	other := patchDiffError(errors.New("dial tcp: refused"))
	if strings.Contains(other, i18n.T("Update T3Go on the environment")) {
		t.Fatalf("a connection failure was called an old server: %q", other)
	}
}
