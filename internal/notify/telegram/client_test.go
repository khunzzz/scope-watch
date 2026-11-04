package telegram

import (
	"errors"
	"fmt"
	"testing"
)

func TestMessageRejectionClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"oversized text", &DeliveryError{Status: 400, Description: "Bad Request: message is too long"}, true},
		{"invalid markup", fmt.Errorf("send: %w", &DeliveryError{Status: 400, Description: "Bad Request: can't parse entities"}), true},
		{"empty text", &DeliveryError{Status: 400, Description: "Bad Request: message text is empty"}, true},
		{"unknown bad request", &DeliveryError{Status: 400, Description: "Bad Request: unknown failure"}, false},
		{"chat missing", &DeliveryError{Status: 400, Description: "Bad Request: chat not found"}, false},
		{"invalid token", &DeliveryError{Status: 401, Description: "Unauthorized"}, false},
		{"blocked bot", &DeliveryError{Status: 403, Description: "Forbidden: bot was blocked"}, false},
		{"rate limited", &DeliveryError{Status: 429, Description: "Too Many Requests"}, false},
		{"server failure", &DeliveryError{Status: 500, Description: "message is too long"}, false},
		{"network error", errors.New("connection reset"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMessageRejected(tc.err); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}
