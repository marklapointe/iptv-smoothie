package store_test

import (
	"path/filepath"
	"testing"

	"github.com/mlapointe/smoothie/internal/store"
)

func TestDefaultAdmin_AdminAdmin(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	u, err := db.Authenticate(store.DefaultAdminUsername, store.DefaultAdminPassword)
	if err != nil {
		t.Fatalf("Authenticate admin:admin: %v", err)
	}
	if u.Username != "admin" {
		t.Fatalf("username = %q", u.Username)
	}
	if _, err := db.Authenticate("admin", "wrong"); err == nil {
		t.Fatal("expected bad password to fail")
	}
}

func TestSetupWizard_RequiredUntilComplete(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st, err := db.GetSetupStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !st.WizardRequired || st.SetupComplete {
		t.Fatalf("expected wizard required on fresh DB, got %+v", st)
	}
	if st.DefaultUser != "admin" {
		t.Errorf("DefaultUser = %q", st.DefaultUser)
	}
	if st.HasSources {
		t.Error("fresh DB should have no sources")
	}

	// Completing setup hides wizard even before sources (user may finish early)
	if err := db.MarkSetupComplete(); err != nil {
		t.Fatal(err)
	}
	st2, err := db.GetSetupStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st2.WizardRequired || !st2.SetupComplete {
		t.Fatalf("expected wizard done, got %+v", st2)
	}
}

func TestUpdatePassword(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpdatePassword("admin", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Authenticate("admin", "admin"); err == nil {
		t.Fatal("old password should fail")
	}
	if _, err := db.Authenticate("admin", "s3cret"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}
