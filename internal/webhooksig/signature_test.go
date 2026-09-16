package webhooksig

import (
	"strconv"
	"testing"
	"time"
)

func TestEnvelopeIntegrityAndReplayWindow(t *testing.T) {
	now := time.Now()
	stamp := strconv.FormatInt(now.Unix(), 10)
	key := []byte("test secret")
	body := []byte(`{"id":1}`)
	signature := Sign(key, stamp, "id-1", "sync.completed", body)
	if !Verify(key, stamp, "id-1", "sync.completed", body, signature, now) {
		t.Fatal("valid signature rejected")
	}
	if Verify(key, stamp, "id-2", "sync.completed", body, signature, now) || Verify(key, stamp, "id-1", "sync.other", body, signature, now) || Verify(key, stamp, "id-1", "sync.completed", []byte(`{}`), signature, now) || Verify(key, stamp, "id-1", "sync.completed", body, signature, now.Add(6*time.Minute)) {
		t.Fatal("tamper or replay accepted")
	}
}
