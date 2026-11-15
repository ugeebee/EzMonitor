package models

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	// emailRegex matches the PostgreSQL schema constraint: ^[^@\s]+@[^@\s]+\.[^@\s]+$
	emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

type User struct {
	UID         int64     `json:"u_id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	HashPass    string    `json:"-"`
	DateCreated time.Time `json:"date_created"`
}

type UserResponse struct {
	UID         int64     `json:"u_id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	DateCreated time.Time `json:"date_created"`
}

func (u *User) ToResponse() UserResponse {
	return UserResponse{
		UID:         u.UID,
		Name:        u.Name,
		Email:       u.Email,
		DateCreated: u.DateCreated,
	}
}

type RegisterRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *RegisterRequest) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("name is required and cannot be empty")
	}
	if len(r.Name) > 100 {
		return errors.New("name cannot exceed 100 characters")
	}

	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if r.Email == "" {
		return errors.New("email is required")
	}
	if !emailRegex.MatchString(r.Email) {
		return errors.New("invalid email format")
	}

	if len(r.Password) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	if len(r.Password) > 72 {
		// bcrypt max length is 72 bytes
		return errors.New("password cannot exceed 72 characters")
	}

	return nil
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r *LoginRequest) Validate() error {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if r.Email == "" {
		return errors.New("email is required")
	}
	if r.Password == "" {
		return errors.New("password is required")
	}
	return nil
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

