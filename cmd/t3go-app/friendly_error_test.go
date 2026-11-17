package main

import (
	"net/http"
	"testing"

	"github.com/tk/t3go/internal/client"
)

func TestFriendlyErrorExplainsRejectedCredentials(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "wrong credentials",
			err: &client.APIError{Status: http.StatusUnauthorized,
				Code: "invalid_credentials", Message: "invalid email or password"},
			want: "That email or password is not right.",
		},
		{
			name: "too many attempts",
			err: &client.APIError{Status: http.StatusTooManyRequests,
				Code: "rate_limited", Message: "too many failed sign-in attempts"},
			want: "Too many sign-in attempts. Try again in a few minutes.",
		},
		{
			name: "rejected token",
			err: &client.APIError{Status: http.StatusUnauthorized,
				Code: "unauthorized", Message: "a valid token is required"},
			want: "That token was rejected. Check the token and try again.",
		},
		{
			name: "rejected token without a reason",
			err:  client.ErrUnauthorized,
			want: "That token was rejected. Check the token and try again.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := friendlyError(test.err); got != test.want {
				t.Fatalf("message = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFriendlyErrorKeepsServerMessage(t *testing.T) {
	err := &client.APIError{Status: http.StatusNotFound, Code: "not_found", Message: "thread is gone"}
	if got := friendlyError(err); got != "thread is gone" {
		t.Fatalf("message = %q", got)
	}
}
