package handler

import "net/http"

// Healthz handles GET /healthz — liveness check.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
