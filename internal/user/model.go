// Package user implements an in-memory user management service with
// administrator notes, exposed over HTTP.
package user

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const maxNoteBodyLen = 5000

var phonePattern = regexp.MustCompile(`^\+?[0-9 ()\-.]{7,20}$`)

// Name is a user's personal name.
type Name struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

// Address is a user's postal address.
type Address struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
}

// Note is an administrator-authored update attached to a user.
type Note struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// User is a managed user record.
type User struct {
	ID        string    `json:"id"`
	Name      Name      `json:"name"`
	Phone     string    `json:"phone"`
	Address   Address   `json:"address"`
	Notes     []Note    `json:"notes"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UserInput holds the client-supplied fields for creating or replacing a user.
// Notes are deliberately excluded: they are managed only via the notes endpoints.
type UserInput struct {
	Name    Name    `json:"name"`
	Phone   string  `json:"phone"`
	Address Address `json:"address"`
}

// NoteInput holds the client-supplied fields for a new note.
type NoteInput struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

// Validate reports whether the input is acceptable.
func (in UserInput) Validate() error {
	if strings.TrimSpace(in.Name.First) == "" {
		return errors.New("name.first is required")
	}
	if strings.TrimSpace(in.Name.Last) == "" {
		return errors.New("name.last is required")
	}
	if in.Phone != "" && !phonePattern.MatchString(in.Phone) {
		return errors.New("phone is invalid")
	}
	return nil
}

// Validate reports whether the input is acceptable.
func (in NoteInput) Validate() error {
	if strings.TrimSpace(in.Author) == "" {
		return errors.New("author is required")
	}
	if strings.TrimSpace(in.Body) == "" {
		return errors.New("body is required")
	}
	if utf8.RuneCountInString(in.Body) > maxNoteBodyLen {
		return errors.New("body is too long")
	}
	return nil
}
