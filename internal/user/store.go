package user

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a user or note does not exist.
var ErrNotFound = errors.New("not found")

// Store is a concurrency-safe in-memory user repository.
// Every method returns copies, so callers can never mutate stored state.
type Store struct {
	mu    sync.RWMutex
	users map[string]*User
	now   func() time.Time
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{users: make(map[string]*User), now: time.Now}
}

// Create adds a new user built from in and returns it.
func (s *Store) Create(in UserInput) User {
	now := s.now().UTC()
	u := &User{
		ID:        newID(),
		Name:      in.Name,
		Phone:     in.Phone,
		Address:   in.Address,
		Notes:     []Note{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	s.users[u.ID] = u
	s.mu.Unlock()
	return clone(u)
}

// List returns all users ordered by creation time.
func (s *Store) List() []User {
	s.mu.RLock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, clone(u))
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Get returns the user with the given id.
func (s *Store) Get(id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return clone(u), nil
}

// Update replaces the user's profile fields. Notes are preserved.
func (s *Store) Update(id string, in UserInput) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	u.Name, u.Phone, u.Address = in.Name, in.Phone, in.Address
	u.UpdatedAt = s.now().UTC()
	return clone(u), nil
}

// Delete removes the user and all of their notes.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return ErrNotFound
	}
	delete(s.users, id)
	return nil
}

// AddNote appends a note to the user and returns it.
func (s *Store) AddNote(userID string, in NoteInput) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return Note{}, ErrNotFound
	}
	now := s.now().UTC()
	n := Note{ID: newID(), Author: in.Author, Body: in.Body, CreatedAt: now}
	u.Notes = append(u.Notes, n)
	u.UpdatedAt = now
	return n, nil
}

// ListNotes returns the user's notes, oldest first.
func (s *Store) ListNotes(userID string) ([]Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return slices.Clone(u.Notes), nil
}

// DeleteNote removes a single note from the user.
func (s *Store) DeleteNote(userID, noteID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok {
		return ErrNotFound
	}
	i := slices.IndexFunc(u.Notes, func(n Note) bool { return n.ID == noteID })
	if i < 0 {
		return ErrNotFound
	}
	u.Notes = slices.Delete(u.Notes, i, i+1)
	u.UpdatedAt = s.now().UTC()
	return nil
}

func clone(u *User) User {
	c := *u
	c.Notes = slices.Clone(u.Notes)
	if c.Notes == nil {
		c.Notes = []Note{}
	}
	return c
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("user: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
