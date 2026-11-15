package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/auth"
	"github.com/ugeebee/EzMonitor/backend/internal/middleware"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type AuthHandler struct {
	db        *pgxpool.Pool
	jwtSecret string
}

func NewAuthHandler(db *pgxpool.Pool, jwtSecret string) *AuthHandler {
	return &AuthHandler{
		db:        db,
		jwtSecret: jwtSecret,
	}
}

// Register creates a new user account and returns an auth token.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	hashPass, err := auth.HashPassword(req.Password)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to hash password"})
		return
	}

	var user models.User
	user.Name = req.Name
	user.Email = req.Email

	query := `
		INSERT INTO users (name, email, hash_pass)
		VALUES ($1, $2, $3)
		RETURNING u_id, date_created
	`

	err = h.db.QueryRow(r.Context(), query, user.Name, user.Email, hashPass).Scan(&user.UID, &user.DateCreated)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// 23505 = unique_violation (duplicate email)
			if pgErr.Code == "23505" {
				respondJSON(w, http.StatusConflict, map[string]string{"error": "A user with this email already exists"})
				return
			}
			// 23514 = check_violation (e.g. invalid email format)
			if pgErr.Code == "23514" {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Email format or name rejected by database constraints"})
				return
			}
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create user account"})
		return
	}

	token, err := auth.GenerateToken(&user, h.jwtSecret, 72*time.Hour)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to generate authentication token"})
		return
	}

	respondJSON(w, http.StatusCreated, models.AuthResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

// Login verifies credentials and returns a JWT token.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var user models.User
	query := `
		SELECT u_id, name, email, hash_pass, date_created
		FROM users
		WHERE email = $1
	`
	err := h.db.QueryRow(r.Context(), query, req.Email).Scan(
		&user.UID,
		&user.Name,
		&user.Email,
		&user.HashPass,
		&user.DateCreated,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid email or password"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error during authentication"})
		return
	}

	if !auth.CheckPasswordHash(req.Password, user.HashPass) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid email or password"})
		return
	}

	token, err := auth.GenerateToken(&user, h.jwtSecret, 72*time.Hour)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to generate authentication token"})
		return
	}

	respondJSON(w, http.StatusOK, models.AuthResponse{
		Token: token,
		User:  user.ToResponse(),
	})
}

// Me returns the profile of the currently authenticated user, along with their group memberships.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetAuthUser(r.Context())
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	var user models.User
	queryUser := `SELECT u_id, name, email, date_created FROM users WHERE u_id = $1`
	err := h.db.QueryRow(r.Context(), queryUser, claims.UID).Scan(
		&user.UID,
		&user.Name,
		&user.Email,
		&user.DateCreated,
	)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "User not found"})
		return
	}

	// Fetch user's groups
	queryGroups := `
		SELECT g.g_id, g.label, g.date_created, ugr.role_id, r.role_name
		FROM groups g
		JOIN user_group_roles ugr ON g.g_id = ugr.g_id
		JOIN roles r ON ugr.role_id = r.role_id
		WHERE ugr.u_id = $1
		ORDER BY g.g_id ASC
	`
	rows, err := h.db.Query(r.Context(), queryGroups, claims.UID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to query user groups"})
		return
	}
	defer rows.Close()

	groups := make([]models.GroupWithRole, 0)
	for rows.Next() {
		var g models.GroupWithRole
		if err := rows.Scan(&g.GID, &g.Label, &g.DateCreated, &g.RoleID, &g.RoleName); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse groups"})
			return
		}
		groups = append(groups, g)
	}

	type ProfileResponse struct {
		User   models.UserResponse    `json:"user"`
		Groups []models.GroupWithRole `json:"groups"`
	}

	respondJSON(w, http.StatusOK, ProfileResponse{
		User:   user.ToResponse(),
		Groups: groups,
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

