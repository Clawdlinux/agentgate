package org

import (
	"errors"
	"testing"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	database, err := agentgatedb.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return NewStore(database)
}

func TestStoreCreateOrgAndAdmin(t *testing.T) {
	t.Parallel()
	store := testStore(t)

	organization, err := store.CreateOrg(t.Context(), "Example Inc.")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	admin, err := store.CreateAdmin(t.Context(), organization.ID, "Admin@Example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if admin.OrgID != organization.ID {
		t.Fatalf("admin OrgID = %q, want %q", admin.OrgID, organization.ID)
	}
	if admin.Email != "admin@example.com" {
		t.Fatalf("admin Email = %q, want normalized email", admin.Email)
	}

	var passwordHash string
	if err := store.db.QueryRow("SELECT password_hash FROM org_admins WHERE id = ?", admin.ID).Scan(&passwordHash); err != nil {
		t.Fatalf("query password hash: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(passwordHash))
	if err != nil {
		t.Fatalf("bcrypt cost: %v", err)
	}
	if cost != passwordHashCost {
		t.Fatalf("bcrypt cost = %d, want %d", cost, passwordHashCost)
	}
}

func TestStoreAuthenticate(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	organization, err := store.CreateOrg(t.Context(), "Example Inc.")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	admin, err := store.CreateAdmin(t.Context(), organization.ID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{name: "valid credentials", email: " ADMIN@example.com ", password: "correct horse battery staple"},
		{name: "unknown email", email: "unknown@example.com", password: "correct horse battery staple", wantErr: ErrInvalidCredentials},
		{name: "wrong password", email: "admin@example.com", password: "wrong password", wantErr: ErrInvalidCredentials},
		{name: "missing password", email: "admin@example.com", wantErr: ErrInvalidCredentials},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := store.Authenticate(t.Context(), test.email, test.password)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Authenticate error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && (actual.ID != admin.ID || actual.OrgID != organization.ID) {
				t.Fatalf("Authenticate admin = %#v, want ID %q org %q", actual, admin.ID, organization.ID)
			}
		})
	}
}

func TestStoreCreateAdminRejectsInvalidOrganizationAndDuplicateEmail(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	organization, err := store.CreateOrg(t.Context(), "Example Inc.")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if _, err := store.CreateAdmin(t.Context(), "missing", "admin@example.com", "password"); !errors.Is(err, ErrOrgNotFound) {
		t.Fatalf("CreateAdmin missing organization error = %v, want %v", err, ErrOrgNotFound)
	}
	if _, err := store.CreateAdmin(t.Context(), organization.ID, "Admin@Example.com", "password"); err != nil {
		t.Fatalf("first CreateAdmin: %v", err)
	}
	if _, err := store.CreateAdmin(t.Context(), organization.ID, "admin@example.com", "password"); err == nil {
		t.Fatal("duplicate normalized email was accepted")
	}
	if _, err := store.CreateAdmin(t.Context(), organization.ID, "long-password@example.com", string(make([]byte, maxPasswordBytes+1))); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("CreateAdmin long password error = %v, want %v", err, ErrPasswordTooLong)
	}
}

func TestStoreAdminCount(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	if count, err := store.AdminCount(t.Context()); err != nil || count != 0 {
		t.Fatalf("AdminCount = (%d, %v), want (0, nil)", count, err)
	}
	organization, err := store.CreateOrg(t.Context(), "Example Inc.")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if _, err := store.CreateAdmin(t.Context(), organization.ID, "admin@example.com", "password"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if count, err := store.AdminCount(t.Context()); err != nil || count != 1 {
		t.Fatalf("AdminCount = (%d, %v), want (1, nil)", count, err)
	}
}

func TestStoreBootstrapAdmin(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	organization, admin, created, err := store.BootstrapAdmin(t.Context(), "Example Inc.", "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if !created || organization == nil || admin == nil || admin.OrgID != organization.ID {
		t.Fatalf("BootstrapAdmin = (%#v, %#v, %t), want created organization and admin", organization, admin, created)
	}
	organization, admin, created, err = store.BootstrapAdmin(t.Context(), "Other Inc.", "other@example.com", "password")
	if err != nil {
		t.Fatalf("second BootstrapAdmin: %v", err)
	}
	if created || organization != nil || admin != nil {
		t.Fatalf("second BootstrapAdmin = (%#v, %#v, %t), want no-op", organization, admin, created)
	}
}
