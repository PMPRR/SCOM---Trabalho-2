package domain

import "time"

// Role is what a user is allowed to do.
type Role string

const (
	// RoleOperator registers stations and manages the ones they own.
	RoleOperator Role = "operator"
	// RoleAdmin manages every station and sees the list of users.
	RoleAdmin Role = "admin"
)

// User is a person with an account. PasswordHash never leaves the server:
// the json tag keeps it out of any response by construction.
type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }
