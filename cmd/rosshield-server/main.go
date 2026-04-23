package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
)

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	return mux
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "bind address")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	slog.Info("server starting", "addr", *addr)

	if err := http.ListenAndServe(*addr, newMux()); err != nil {
		slog.Error("server failed", "err", err.Error())
		os.Exit(1)
	}
}
