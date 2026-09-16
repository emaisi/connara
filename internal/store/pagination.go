package store

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type ListOptions struct {
	Limit         int
	Before        time.Time
	ID            string
	Query         string
	Status        string
	Executable    string
	Group         string
	Scope         string
	IntegrationID string
	ConnectionID  string
	SyncTaskID    string
	From          time.Time
	To            time.Time
}

func DecodeCursor(value string) (time.Time, string, error) {
	if value == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 || len(parts[1]) != 36 {
		return time.Time{}, "", errors.New("invalid list cursor")
	}
	if parts[1][8] != '-' || parts[1][13] != '-' || parts[1][18] != '-' || parts[1][23] != '-' {
		return time.Time{}, "", errors.New("invalid cursor ID")
	}
	if decoded, err := hex.DecodeString(strings.ReplaceAll(parts[1], "-", "")); err != nil || len(decoded) != 16 {
		return time.Time{}, "", errors.New("invalid cursor ID")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, parts[0])
	return timestamp, parts[1], err
}
func EncodeCursor(timestamp time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(timestamp.UTC().Format(time.RFC3339Nano) + "|" + id))
}
func listOptions(options []ListOptions) ListOptions {
	if len(options) == 0 {
		return ListOptions{Limit: 2147483647}
	}
	option := options[0]
	if option.Limit < 1 || option.Limit > 200 {
		option.Limit = 100
	}
	return option
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableCursorID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
