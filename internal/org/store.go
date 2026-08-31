package org

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	passwordHashCost = 10
	maxPasswordBytes = 72
)

var (
	ErrInvalidCredentials = errors.New("org: invalid credentials")
	ErrInvalidOrgName     = errors.New("org: organization name is required")
	ErrInvalidAdmin       = errors.New("org: admin email and password are required")
	ErrPasswordTooLong    = errors.New("org: password exceeds bcrypt's 72-byte limit")
	ErrOrgNotFound        = errors.New("org: organization not found")
)

type Org struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Admin struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateOrg(ctx context.Context, name string) (*Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidOrgName
	}

	organization := &Org{ID: generateID(), Name: name}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO orgs (id, name) VALUES (?, ?)", organization.ID, organization.Name); err != nil {
		return nil, fmt.Errorf("org.CreateOrg: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT created_at FROM orgs WHERE id = ?", organization.ID).Scan(&organization.CreatedAt); err != nil {
		return nil, fmt.Errorf("org.CreateOrg: read created_at: %w", err)
	}
	return organization, nil
}

func (s *Store) CreateAdmin(ctx context.Context, orgID, email, password string) (*Admin, error) {
	email = normalizeEmail(email)
	if orgID == "" || email == "" || password == "" {
		return nil, ErrInvalidAdmin
	}
	if len([]byte(password)) > maxPasswordBytes {
		return nil, ErrPasswordTooLong
	}

	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM orgs WHERE id = ?)", orgID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("org.CreateAdmin: check organization: %w", err)
	}
	if !exists {
		return nil, ErrOrgNotFound
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
	if err != nil {
		return nil, fmt.Errorf("org.CreateAdmin: hash password: %w", err)
	}

	admin := &Admin{ID: generateID(), OrgID: orgID, Email: email}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO org_admins (id, org_id, email, password_hash) VALUES (?, ?, ?, ?)", admin.ID, admin.OrgID, admin.Email, string(passwordHash)); err != nil {
		return nil, fmt.Errorf("org.CreateAdmin: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT created_at FROM org_admins WHERE id = ?", admin.ID).Scan(&admin.CreatedAt); err != nil {
		return nil, fmt.Errorf("org.CreateAdmin: read created_at: %w", err)
	}
	return admin, nil
}

func (s *Store) BootstrapAdmin(ctx context.Context, organizationName, email, password string) (*Org, *Admin, bool, error) {
	organizationName = strings.TrimSpace(organizationName)
	email = normalizeEmail(email)
	if organizationName == "" {
		return nil, nil, false, ErrInvalidOrgName
	}
	if email == "" || password == "" {
		return nil, nil, false, ErrInvalidAdmin
	}
	if len([]byte(password)) > maxPasswordBytes {
		return nil, nil, false, ErrPasswordTooLong
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
	if err != nil {
		return nil, nil, false, fmt.Errorf("org.BootstrapAdmin: hash password: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("org.BootstrapAdmin: begin transaction: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM org_admins").Scan(&count); err != nil {
		return nil, nil, false, fmt.Errorf("org.BootstrapAdmin: count admins: %w", err)
	}
	if count > 0 {
		if err := tx.Commit(); err != nil {
			return nil, nil, false, fmt.Errorf("org.BootstrapAdmin: commit existing admins: %w", err)
		}
		return nil, nil, false, nil
	}

	organization := &Org{ID: generateID(), Name: organizationName}
	if _, err := tx.ExecContext(ctx, "INSERT INTO orgs (id, name) VALUES (?, ?)", organization.ID, organization.Name); err != nil {
		return nil, nil, false, fmt.Errorf("org.BootstrapAdmin: create organization: %w", err)
	}
	admin := &Admin{ID: generateID(), OrgID: organization.ID, Email: email}
	if _, err := tx.ExecContext(ctx, "INSERT INTO org_admins (id, org_id, email, password_hash) VALUES (?, ?, ?, ?)", admin.ID, admin.OrgID, admin.Email, string(passwordHash)); err != nil {
		return nil, nil, false, fmt.Errorf("org.BootstrapAdmin: create admin: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, fmt.Errorf("org.BootstrapAdmin: commit: %w", err)
	}
	return organization, admin, true, nil
}

func (s *Store) AdminCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM org_admins").Scan(&count); err != nil {
		return 0, fmt.Errorf("org.AdminCount: %w", err)
	}
	return count, nil
}

func (s *Store) Authenticate(ctx context.Context, email, password string) (*Admin, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" || len([]byte(password)) > maxPasswordBytes {
		return nil, ErrInvalidCredentials
	}

	var admin Admin
	var passwordHash string
	err := s.db.QueryRowContext(ctx, "SELECT id, org_id, email, password_hash, created_at FROM org_admins WHERE email = ?", email).Scan(&admin.ID, &admin.OrgID, &admin.Email, &passwordHash, &admin.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("org.Authenticate: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return &admin, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("org: generate ID: %v", err))
	}
	return hex.EncodeToString(bytes)
}
