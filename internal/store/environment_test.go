package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEnvironmentIdentitySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.EnvironmentIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	second, err := s.EnvironmentIdentity(context.Background())
	if err != nil || first.ID != second.ID || first.PublicKey() != second.PublicKey() {
		t.Fatal("identity changed", err)
	}
}
