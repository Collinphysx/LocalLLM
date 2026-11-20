package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const adminToken = "secret"

const validUser = `{"name":{"first":"Ada","last":"Lovelace"},"phone":"+1 555 0100",
	"address":{"street":"1 Main St","city":"London","state":"","postalCode":"N1","country":"UK"}}`

func do(t *testing.T, h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func createUser(t *testing.T, h http.Handler) User {
	t.Helper()
	rec := do(t, h, "POST", "/users", validUser, "")
	wantStatus(t, rec, http.StatusCreated)
	return decodeBody[User](t, rec)
}

func TestCreateUser(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	rec := do(t, h, "POST", "/users", validUser, "")
	wantStatus(t, rec, http.StatusCreated)

	u := decodeBody[User](t, rec)
	if u.ID == "" || u.Name.First != "Ada" || u.Address.City != "London" || u.Phone != "+1 555 0100" {
		t.Errorf("unexpected user: %+v", u)
	}
	if got := rec.Header().Get("Location"); got != "/users/"+u.ID {
		t.Errorf("Location = %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"notes":[]`) {
		t.Errorf("notes should serialize as [], got %s", rec.Body.String())
	}
}

func TestCreateUserBadRequest(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	cases := map[string]string{
		"missing first": `{"name":{"last":"Lovelace"}}`,
		"missing last":  `{"name":{"first":"Ada"}}`,
		"blank first":   `{"name":{"first":"  ","last":"Lovelace"}}`,
		"bad phone":     `{"name":{"first":"Ada","last":"Lovelace"},"phone":"call me"}`,
		"unknown field": `{"name":{"first":"Ada","last":"Lovelace"},"notes":[]}`,
		"malformed":     `{"name":`,
		"two objects":   `{"name":{"first":"Ada","last":"L"}}{}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(t, h, "POST", "/users", body, "")
			wantStatus(t, rec, http.StatusBadRequest)
			if decodeBody[map[string]string](t, rec)["error"] == "" {
				t.Error("missing error message")
			}
		})
	}
}

func TestListAndGetUsers(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	a, b := createUser(t, h), createUser(t, h)

	rec := do(t, h, "GET", "/users", "", "")
	wantStatus(t, rec, http.StatusOK)
	if users := decodeBody[[]User](t, rec); len(users) != 2 {
		t.Fatalf("len = %d, want 2", len(users))
	}

	rec = do(t, h, "GET", "/users/"+b.ID, "", "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeBody[User](t, rec); got.ID != b.ID || got.ID == a.ID {
		t.Errorf("got id %q, want %q", got.ID, b.ID)
	}

	wantStatus(t, do(t, h, "GET", "/users/nope", "", ""), http.StatusNotFound)
}

func TestUpdateUserPreservesNotes(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	wantStatus(t, do(t, h, "POST", "/users/"+u.ID+"/notes", `{"author":"admin","body":"verified"}`, adminToken), http.StatusCreated)

	rec := do(t, h, "PUT", "/users/"+u.ID, `{"name":{"first":"Grace","last":"Hopper"},"phone":"555-0199"}`, "")
	wantStatus(t, rec, http.StatusOK)
	got := decodeBody[User](t, rec)
	if got.Name.First != "Grace" || got.Phone != "555-0199" || got.Address.City != "" {
		t.Errorf("update not applied as full replace: %+v", got)
	}
	if len(got.Notes) != 1 {
		t.Errorf("notes = %d, want 1 (preserved)", len(got.Notes))
	}
	if !got.UpdatedAt.After(u.UpdatedAt) && !got.UpdatedAt.Equal(u.UpdatedAt) {
		t.Errorf("UpdatedAt went backwards")
	}

	wantStatus(t, do(t, h, "PUT", "/users/nope", validUser, ""), http.StatusNotFound)
	wantStatus(t, do(t, h, "PUT", "/users/"+u.ID, `{"name":{}}`, ""), http.StatusBadRequest)
}

func TestDeleteUser(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	wantStatus(t, do(t, h, "DELETE", "/users/"+u.ID, "", ""), http.StatusNoContent)
	wantStatus(t, do(t, h, "GET", "/users/"+u.ID, "", ""), http.StatusNotFound)
	wantStatus(t, do(t, h, "DELETE", "/users/"+u.ID, "", ""), http.StatusNotFound)
}

func TestNotesAuth(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	path := "/users/" + u.ID + "/notes"
	note := `{"author":"admin","body":"hi"}`

	for _, tc := range []struct {
		name, method, token string
	}{
		{"list no token", "GET", ""},
		{"list wrong token", "GET", "wrong"},
		{"add no token", "POST", ""},
		{"add wrong token", "POST", "wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, tc.method, path, note, tc.token)
			wantStatus(t, rec, http.StatusUnauthorized)
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate header")
			}
		})
	}

	t.Run("disabled when no admin token configured", func(t *testing.T) {
		h := NewHandler(NewStore(), "")
		u := createUser(t, h)
		wantStatus(t, do(t, h, "POST", "/users/"+u.ID+"/notes", note, ""), http.StatusForbidden)
		wantStatus(t, do(t, h, "POST", "/users/"+u.ID+"/notes", note, "anything"), http.StatusForbidden)
	})
}

func TestNotesLifecycle(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	path := "/users/" + u.ID + "/notes"

	rec := do(t, h, "POST", path, `{"author":"admin","body":"Called to confirm address."}`, adminToken)
	wantStatus(t, rec, http.StatusCreated)
	n := decodeBody[Note](t, rec)
	if n.ID == "" || n.Author != "admin" || n.CreatedAt.IsZero() {
		t.Errorf("unexpected note: %+v", n)
	}
	wantStatus(t, do(t, h, "POST", path, `{"author":"admin","body":"Second."}`, adminToken), http.StatusCreated)

	rec = do(t, h, "GET", path, "", adminToken)
	wantStatus(t, rec, http.StatusOK)
	notes := decodeBody[[]Note](t, rec)
	if len(notes) != 2 || notes[0].ID != n.ID {
		t.Fatalf("notes = %+v", notes)
	}

	// Notes are also visible on the user record.
	if got := decodeBody[User](t, do(t, h, "GET", "/users/"+u.ID, "", "")); len(got.Notes) != 2 {
		t.Errorf("user.notes = %d, want 2", len(got.Notes))
	}

	wantStatus(t, do(t, h, "DELETE", path+"/"+n.ID, "", adminToken), http.StatusNoContent)
	wantStatus(t, do(t, h, "DELETE", path+"/"+n.ID, "", adminToken), http.StatusNotFound)
	if notes := decodeBody[[]Note](t, do(t, h, "GET", path, "", adminToken)); len(notes) != 1 {
		t.Errorf("after delete len = %d, want 1", len(notes))
	}
}

func TestNotesErrors(t *testing.T) {
	h := NewHandler(NewStore(), adminToken)
	u := createUser(t, h)
	path := "/users/" + u.ID + "/notes"

	wantStatus(t, do(t, h, "POST", "/users/nope/notes", `{"author":"a","body":"b"}`, adminToken), http.StatusNotFound)
	wantStatus(t, do(t, h, "GET", "/users/nope/notes", "", adminToken), http.StatusNotFound)
	wantStatus(t, do(t, h, "DELETE", "/users/nope/notes/x", "", adminToken), http.StatusNotFound)
	wantStatus(t, do(t, h, "POST", path, `{"author":"","body":"b"}`, adminToken), http.StatusBadRequest)
	wantStatus(t, do(t, h, "POST", path, `{"author":"a","body":" "}`, adminToken), http.StatusBadRequest)
}

func TestUserInputValidate(t *testing.T) {
	ok := Name{First: "Ada", Last: "Lovelace"}
	cases := []struct {
		name    string
		in      UserInput
		wantErr bool
	}{
		{"valid no phone", UserInput{Name: ok}, false},
		{"valid phone", UserInput{Name: ok, Phone: "+44 (20) 7946-0958"}, false},
		{"missing first", UserInput{Name: Name{Last: "L"}}, true},
		{"missing last", UserInput{Name: Name{First: "A"}}, true},
		{"phone too short", UserInput{Name: ok, Phone: "123"}, true},
		{"phone letters", UserInput{Name: ok, Phone: "555-CALL-NOW"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.in.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestNoteInputValidate(t *testing.T) {
	cases := []struct {
		name    string
		in      NoteInput
		wantErr bool
	}{
		{"valid", NoteInput{Author: "admin", Body: "ok"}, false},
		{"max length", NoteInput{Author: "admin", Body: strings.Repeat("é", maxNoteBodyLen)}, false},
		{"too long", NoteInput{Author: "admin", Body: strings.Repeat("é", maxNoteBodyLen+1)}, true},
		{"no author", NoteInput{Body: "ok"}, true},
		{"no body", NoteInput{Author: "admin"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.in.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestStoreReturnsCopies(t *testing.T) {
	s := NewStore()
	u := s.Create(UserInput{Name: Name{First: "A", Last: "B"}})
	if _, err := s.AddNote(u.ID, NoteInput{Author: "a", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(u.ID)
	got.Notes[0].Body = "mutated"
	got.Name.First = "mutated"

	again, _ := s.Get(u.ID)
	if again.Notes[0].Body != "b" || again.Name.First != "A" {
		t.Errorf("store state was mutated through a returned copy: %+v", again)
	}
}
