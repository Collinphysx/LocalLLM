package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestListUsersEmpty(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	rec := do(t, h, "GET", "/users", "", "")
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "[]\n" {
		t.Errorf("expected [], got %q", rec.Body.String())
	}
}

func TestListUsersOrder(t *testing.T) {
	// Fake clock: each call is one second later, so creation order is unambiguous.
	s := NewStore()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	h := NewHandler(s, adminToken)

	u1 := createUser(t, h)
	u2 := createUser(t, h)
	u3 := createUser(t, h)

	rec := do(t, h, "GET", "/users", "", "")
	wantStatus(t, rec, http.StatusOK)
	users := decodeBody[[]User](t, rec)
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}
	if users[0].ID != u1.ID || users[1].ID != u2.ID || users[2].ID != u3.ID {
		t.Errorf("users not in creation order: %+v", users)
	}
}

func TestResponseHeaders(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	cases := []struct {
		method, path, body, token string
		wantStatus               int
	}{
		{"POST", "/users", validUser, "", http.StatusCreated},
		{"GET", "/users", "", "", http.StatusOK},
		{"POST", "/users", `{"name":{"first":""}}`, "", http.StatusBadRequest},
		{"GET", "/users/nope", "", "", http.StatusNotFound},
	}

	for _, tc := range cases {
		rec := do(t, h, tc.method, tc.path, tc.body, tc.token)
		wantStatus(t, rec, tc.wantStatus)
		if rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json for status %d", tc.wantStatus)
		}
	}
}

func TestRequestBodyTooLarge(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	// Valid JSON prefix so the decoder keeps reading until it hits the size limit.
	largeBody := `{"name":{"first":"` + strings.Repeat("a", maxBodyBytes+1) + `","last":"L"}}`
	rec := do(t, h, "POST", "/users", largeBody, "")
	wantStatus(t, rec, http.StatusRequestEntityTooLarge)
}

func TestUpdateUserBadRequest(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)

	cases := []struct {
		name string
		body string
	}{
		{"malformed", `{"name":{"first":"X"`},
		{"unknown field", `{"name":{"first":"X","last":"Y"},"notes":[]}`,
		{"bad phone", `{"name":{"first":"X","last":"Y"},"phone":"bad"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, "PUT", "/users/"+u.ID, tc.body, "")
			wantStatus(t, rec, http.StatusBadRequest)

			rec2 := do(t, h, "GET", "/users/"+u.ID, "", "")
			wantStatus(t, rec2, http.StatusOK)
			got := decodeBody[User](t, rec2)
			if got.Name.First != u.Name.First || got.Phone != u.Phone {
				t.Errorf("user was mutated on bad request: got %+v, want %+v", got, u)
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)

	rec1 := do(t, h, "PATCH", "/users/"+u.ID, "", "")
	wantStatus(t, rec1, http.StatusMethodNotAllowed)

	rec2 := do(t, h, "PUT", "/users", validUser, "")
	wantStatus(t, rec2, http.StatusMethodNotAllowed)
}

func TestAdminAuthHeaderVariants(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	path := "/users/" + u.ID + "/notes"
	body := `{"author":"admin","body":"test"}`

	tests := []struct {
		name string
		auth string
		want int
	}{
		{"valid", "Bearer secret", http.StatusCreated},
		{"lowercase", "bearer secret", http.StatusUnauthorized},
		{"double space", "Bearer  secret", http.StatusUnauthorized},
		{"basic", "Basic secret", http.StatusUnauthorized},
		{"no bearer", "secret", http.StatusUnauthorized},
		{"wrong token", "Bearer secretX", http.StatusUnauthorized},
		{"bearer only", "Bearer ", http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			wantStatus(t, rec, tc.want)
		})
	}
}

func TestDeleteNoteAuthAndMissing(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	path := "/users/" + u.ID + "/notes"

	rec1 := do(t, h, "DELETE", path+"/note1", "", "")
	wantStatus(t, rec1, http.StatusUnauthorized)

	rec2 := do(t, h, "DELETE", path+"/nonexistent", "", adminToken)
	wantStatus(t, rec2, http.StatusNotFound)
}

func TestDeleteUserRemovesNotes(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	path := "/users/" + u.ID + "/notes"

	wantStatus(t, do(t, h, "POST", path, `{"author":"admin","body":"test"}`, adminToken), http.StatusCreated)
	wantStatus(t, do(t, h, "DELETE", "/users/"+u.ID, "", ""), http.StatusNoContent)

	rec := do(t, h, "GET", path, "", adminToken)
	wantStatus(t, rec, http.StatusNotFound)
}

func TestAddNoteBumpsUpdatedAt(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)

	rec := do(t, h, "POST", "/users/"+u.ID+"/notes", `{"author":"admin","body":"test"}`, adminToken)
	wantStatus(t, rec, http.StatusCreated)

	got := decodeBody[User](t, do(t, h, "GET", "/users/"+u.ID, "", ""))
	if got.UpdatedAt.Before(u.UpdatedAt) {
		t.Errorf("UpdatedAt %v is before original %v", got.UpdatedAt, u.UpdatedAt)
	}
	if len(got.Notes) != 1 {
		t.Errorf("expected 1 note, got %d", len(got.Notes))
	}
}

func TestNoteBodyLimitViaHTTP(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	path := "/users/" + u.ID + "/notes"

	bodyMax := `{"author":"admin","body":"` + strings.Repeat("a", maxNoteBodyLen) + `"}`
	recMax := do(t, h, "POST", path, bodyMax, adminToken)
	wantStatus(t, recMax, http.StatusCreated)

	bodyOver := `{"author":"admin","body":"` + strings.Repeat("a", maxNoteBodyLen+1) + `"}`
	recOver := do(t, h, "POST", path, bodyOver, adminToken)
	wantStatus(t, recOver, http.StatusBadRequest)
}

func TestConcurrentAccess(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	var wg sync.WaitGroup
	count := 50

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reqCreate := httptest.NewRequest("POST", "/users", strings.NewReader(validUser))
			reqCreate.Header.Set("Content-Type", "application/json")
			recCreate := httptest.NewRecorder()
			h.ServeHTTP(recCreate, reqCreate)

			if recCreate.Code != http.StatusCreated {
				t.Errorf("expected 201, got %d", recCreate.Code)
				return
			}

			var u User
			if err := json.Unmarshal(recCreate.Body.Bytes(), &u); err != nil {
				t.Errorf("decode created user: %v", err)
				return
			}

			reqNote := httptest.NewRequest("POST", "/users/"+u.ID+"/notes", strings.NewReader(`{"author":"admin","body":"test"}`))
			reqNote.Header.Set("Content-Type", "application/json")
			reqNote.Header.Set("Authorization", "Bearer "+adminToken)
			recNote := httptest.NewRecorder()
			h.ServeHTTP(recNote, reqNote)

			if recNote.Code != http.StatusCreated {
				t.Errorf("expected 201 for note, got %d", recNote.Code)
			}
		}()
	}
	wg.Wait()

	rec := do(t, h, "GET", "/users", "", "")
	wantStatus(t, rec, http.StatusOK)
	users := decodeBody[[]User](t, rec)
	if len(users) != count {
		t.Errorf("expected %d users, got %d", count, len(users))
	}
	for _, u := range users {
		if len(u.Notes) != 1 {
			t.Errorf("user %s has %d notes, want 1", u.ID, len(u.Notes))
		}
	}
}