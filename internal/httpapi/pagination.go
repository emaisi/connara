package httpapi

import (
	"apihub-go/internal/store"
	"net/http"
	"time"
)

func listOptions(w http.ResponseWriter, r *http.Request) (store.ListOptions, bool) {
	before, id, err := store.DecodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeAdminError(w, 400, "invalid_cursor", "Invalid page cursor")
		return store.ListOptions{}, false
	}
	var from, to time.Time
	for _, bound := range []struct {
		name   string
		target *time.Time
	}{{"from", &from}, {"to", &to}} {
		if value := r.URL.Query().Get(bound.name); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				writeAdminError(w, 400, "invalid_input", "时间范围必须使用 RFC3339 格式")
				return store.ListOptions{}, false
			}
			*bound.target = parsed
		}
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		writeAdminError(w, 400, "invalid_input", "开始时间不能晚于结束时间")
		return store.ListOptions{}, false
	}
	return store.ListOptions{Executable: r.URL.Query().Get("executable"), Group: r.URL.Query().Get("group"), Scope: r.URL.Query().Get("scope"), IntegrationID: r.URL.Query().Get("integration"), ConnectionID: r.URL.Query().Get("connection"), SyncTaskID: r.URL.Query().Get("task"), From: from, To: to, Limit: parseLimit(r, 100, 200), Before: before, ID: id, Query: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status")}, true
}
func nextCursor(w http.ResponseWriter, count, limit int, at time.Time, id string) {
	if count == limit {
		w.Header().Set("X-Next-Cursor", store.EncodeCursor(at, id))
	}
}
