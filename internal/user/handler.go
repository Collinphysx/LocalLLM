package user

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Handler serves the user management API.
//
//	POST   /users                         create a user
//	GET    /users                         list users
//	GET    /users/{id}                    get a user (includes notes)
//	PUT    /users/{id}                    replace a user's name, phone, address
//	DELETE /users/{id}                    delete a user
//	GET    /users/{id}/notes              list notes          (admin)
//	POST   /users/{id}/notes              add a note          (admin)
//	DELETE /users/{id}/notes/{noteId}     delete a note       (admin)
//
// Admin routes require "Authorization: Bearer <adminToken>". If adminToken
// is empty, admin routes are disabled and always return 403.
type Handler struct {
	store      *Store
	adminToken string
	mux        *http.ServeMux
}

// NewHandler wires the routes onto a new ServeMux.
func NewHandler(store *Store, adminToken string) *Handler {
	h := &Handler{store: store, adminToken: adminToken, mux: http.NewServeMux()}

	h.mux.HandleFunc("POST /users", h.createUser)
	h.mux.HandleFunc("GET /users", h.listUsers)
	h.mux.HandleFunc("GET /users/{id}", h.getUser)
	h.mux.HandleFunc("PUT /users/{id}", h.updateUser)
	h.mux.HandleFunc("DELETE /users/{id}", h.deleteUser)

	h.mux.HandleFunc("GET /users/{id}/notes", h.requireAdmin(h.listNotes))
	h.mux.HandleFunc("POST /users/{id}/notes", h.requireAdmin(h.addNote))
	h.mux.HandleFunc("DELETE /users/{id}/notes/{noteId}", h.requireAdmin(h.deleteNote))

	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// --- users ---

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var in UserInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := h.store.Create(in)
	w.Header().Set("Location", "/users/"+u.ID)
	writeJSON(w, http.StatusCreated, u)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.List())
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	var in UserInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := h.store.Update(r.PathValue("id"), in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- notes (admin) ---

func (h *Handler) listNotes(w http.ResponseWriter, r *http.Request) {
	notes, err := h.store.ListNotes(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

func (h *Handler) addNote(w http.ResponseWriter, r *http.Request) {
	var in NoteInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := h.store.AddNote(r.PathValue("id"), in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request) {
	if err := h.store.DeleteNote(r.PathValue("id"), r.PathValue("noteId")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.adminToken == "" {
			writeError(w, http.StatusForbidden, "admin access is disabled")
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(h.adminToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="admin"`)
			writeError(w, http.StatusUnauthorized, "admin token required")
			return
		}
		next(w, r)
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		}
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return false
	}
	return true
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
