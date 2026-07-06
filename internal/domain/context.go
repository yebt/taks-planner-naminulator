package domain

import "time"

// Note is one accumulated piece of context on a project or person (info,
// a decision made, a change that happened, …).
type Note struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"` // info | decision | change (free-form)
	Text string    `json:"text"`
}

// Project is something tasks refer to with the +slug prefix (e.g. +GarageSale).
// It carries context so the agent can draft tasks coherently for it.
type Project struct {
	Slug        string // referenced as +slug (case-insensitive)
	Name        string
	Description string // one-line summary (stack, purpose, …)
	Notes       []Note
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Person is someone tasks refer to with the @nick prefix (e.g. @kari).
type Person struct {
	Nick      string // referenced as @nick (case-insensitive)
	Name      string
	Role      string // area / role, e.g. "área comercial"
	Notes     []Note
	CreatedAt time.Time
	UpdatedAt time.Time
}
