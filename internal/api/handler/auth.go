package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/CoolBanHub/ailens360/internal/api/response"
	"github.com/CoolBanHub/ailens360/internal/auth"
)

type loginIn struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var in loginIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Error(w, http.StatusBadRequest, 40000, "invalid body")
		return
	}
	if err := h.Auth.Login(in.Username, in.Password); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			response.Error(w, http.StatusUnauthorized, 40100, "invalid username or password")
			return
		}
		response.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	tok, exp, err := h.Auth.Issue(in.Username)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	response.OK(w, map[string]any{
		"token":      tok,
		"expires_at": exp.Unix(),
		"username":   in.Username,
	})
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	// AdminJWT middleware already validated; we just echo the subject by
	// re-verifying the header. Avoids stashing it in a context for now.
	raw := r.Header.Get("Authorization")
	if len(raw) > len("Bearer ") {
		raw = raw[len("Bearer "):]
	}
	sub, _ := h.Auth.Verify(raw)
	response.OK(w, map[string]any{"username": sub})
}
