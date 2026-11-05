package app

import (
	"os"
	"strings"
	"testing"
)

func TestMessageThreadIDFromEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TG_BOT_TOKEN", "")
	t.Setenv("TG_BOT_TOKEN_FILE", "")
	t.Setenv("TG_CHAT_IDS", "")
	for _, tc := range []struct {
		name    string
		value   *string
		want    int
		invalid bool
	}{
		{name: "unset"},
		{name: "topic", value: strptr("4"), want: 4},
		{name: "zero", value: strptr("0"), invalid: true},
		{name: "negative", value: strptr("-4"), invalid: true},
		{name: "empty", value: strptr(""), invalid: true},
		{name: "malformed", value: strptr("topic"), invalid: true},
		{name: "fraction", value: strptr("4.5"), invalid: true},
		{name: "whitespace", value: strptr(" 4 "), invalid: true},
		{name: "overflow", value: strptr("9223372036854775808"), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TG_MESSAGE_THREAD_ID", "")
			if tc.value == nil {
				if err := os.Unsetenv("TG_MESSAGE_THREAD_ID"); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("TG_MESSAGE_THREAD_ID", *tc.value)
			}
			settings, err := FromEnv()
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "TG_MESSAGE_THREAD_ID") {
					t.Fatalf("want topic validation error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := settings.MessageThreadID; got != tc.want {
				t.Fatalf("topic=%d want=%d", got, tc.want)
			}
		})
	}
}

func strptr(s string) *string { return &s }
