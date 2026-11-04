package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Settings struct {
	Token, ChatID, DataDir, Heartbeat             string
	RequestDelay, Interval                        time.Duration
	FailAlertAfter, MinDrop, MissingRuns, MassMin int
	MinRatio, MaxFailShare, MassRatio             float64
	HistoryDays                                   int
}

func Defaults() Settings {
	return Settings{
		DataDir:        "data",
		RequestDelay:   400 * time.Millisecond,
		Interval:       900 * time.Second,
		FailAlertAfter: 3,
		MinDrop:        5,
		MissingRuns:    2,
		MassMin:        10,
		MinRatio:       .7,
		MaxFailShare:   .2,
		MassRatio:      .3,
	}
}

func (s Settings) HasTelegram() bool { return s.Token != "" && s.ChatID != "" }

func (s Settings) String() string {
	return fmt.Sprintf("Settings{DataDir:%q Interval:%s Telegram:%t}", s.DataDir, s.Interval, s.HasTelegram())
}

func (s Settings) GoString() string { return s.String() }

func LoadDotEnv(path string) error {
	err := godotenv.Load(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func FromEnv() (Settings, error) {
	s := Defaults()
	if e := LoadDotEnv(".env"); e != nil {
		return s, fmt.Errorf("load .env: %w", e)
	}
	s.Token = os.Getenv("TG_BOT_TOKEN")
	if p := os.Getenv("TG_BOT_TOKEN_FILE"); p != "" {
		b, e := os.ReadFile(p)
		if e != nil {
			return s, fmt.Errorf("read Telegram secret: %w", e)
		}
		s.Token = strings.TrimSpace(string(b))
	}
	s.ChatID = os.Getenv("TG_CHAT_ID")
	s.Heartbeat = os.Getenv("SCOPEWATCH_HEARTBEAT")
	if v := os.Getenv("SCOPEWATCH_DATA"); v != "" {
		s.DataDir = filepath.Clean(v)
	}
	for _, item := range []struct {
		name     string
		dst      *time.Duration
		positive bool
	}{
		{"SCOPEWATCH_DELAY", &s.RequestDelay, false},
		{"SCOPEWATCH_INTERVAL", &s.Interval, true},
	} {
		if v := os.Getenv(item.name); v != "" {
			n, e := strconv.ParseFloat(v, 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 86400*365 || (item.positive && n == 0) {
				return s, fmt.Errorf("%s must be valid seconds", item.name)
			}
			*item.dst = time.Duration(n * float64(time.Second))
		}
	}
	if v := os.Getenv("SCOPEWATCH_FAIL_ALERT"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 {
			return s, fmt.Errorf("SCOPEWATCH_FAIL_ALERT must be positive")
		}
		s.FailAlertAfter = n
	}
	if v := os.Getenv("SCOPEWATCH_HISTORY_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 36500 {
			return s, fmt.Errorf("SCOPEWATCH_HISTORY_DAYS must be between 0 and 36500")
		}
		s.HistoryDays = n
	}
	return s, nil
}
