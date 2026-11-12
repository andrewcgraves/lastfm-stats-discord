package bot

import (
	"encoding/json"
	"net/http"
)

// HealthHandler serves /healthz: 200 while the gateway is connected and
// heartbeating, 503 otherwise.
func (b *Bot) HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		st := b.Healthy()
		w.Header().Set("Content-Type", "application/json")
		if !st.Connected {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(st)
	})
	return mux
}
