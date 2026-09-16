package webhooksig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

func Sign(key []byte, timestamp, id, kind string, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("v1\n" + timestamp + "\n" + id + "\n" + kind + "\n"))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}
func Verify(key []byte, timestamp, id, kind string, body []byte, signature string, now time.Time) bool {
	if len(key) == 0 || id == "" || kind == "" || len(id) > 512 || len(kind) > 160 || strings.ContainsAny(id+kind, "\r\n") {
		return false
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != timestamp {
		return false
	}
	delta := now.Sub(time.Unix(seconds, 0))
	if delta > 5*time.Minute || delta < -5*time.Minute {
		return false
	}
	return hmac.Equal([]byte(signature), []byte(Sign(key, timestamp, id, kind, body)))
}
