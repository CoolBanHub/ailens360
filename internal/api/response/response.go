package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func OK(w http.ResponseWriter, data any) {
	write(w, http.StatusOK, Envelope{Code: 0, Message: "ok", Data: data})
}

func Created(w http.ResponseWriter, data any) {
	write(w, http.StatusCreated, Envelope{Code: 0, Message: "ok", Data: data})
}

func Error(w http.ResponseWriter, status, code int, msg string) {
	write(w, status, Envelope{Code: code, Message: msg})
}

func write(w http.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		slog.Default().Error("response encode failed", "err", err)
	}
}
