package httpapi

import "net/http"

func (a *api) catalogLookups(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.CatalogLookups(r.Context())
	writeStoreResult(w, items, err, "load catalog selectors")
}
