package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretAndSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join(t.TempDir(), "token")
	os.WriteFile(path, []byte("secret-token\n"), 0600)
	t.Setenv("TG_BOT_TOKEN_FILE", path)
	t.Setenv("TG_BOT_TOKEN", "ignored")
	s, e := FromEnv()
	if e != nil || s.Token != "secret-token" {
		t.Fatal(e)
	}
	for _, text := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s)} {
		if strings.Contains(text, "secret-token") {
			t.Fatal("token in formatting")
		}
	}
	t.Setenv("TG_BOT_TOKEN_FILE", path+"missing")
	if _, e = FromEnv(); e == nil {
		t.Fatal("missing secret accepted")
	}
}
func TestInvalidSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TG_BOT_TOKEN_FILE", "")
	for _, v := range []string{"bad", "-1", "0", "NaN", "+Inf"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("SCOPEWATCH_INTERVAL", v)
			if _, e := FromEnv(); e == nil {
				t.Fatal("invalid interval", v)
			}
		})
	}
}
func TestDotEnvDoesNotOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TG_CHAT_ID", "existing")
	os.WriteFile(".env", []byte("TG_CHAT_ID=other\n"), 0600)
	if e := LoadDotEnv(".env"); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("TG_CHAT_ID") != "existing" {
		t.Fatal("env overwritten")
	}
}
