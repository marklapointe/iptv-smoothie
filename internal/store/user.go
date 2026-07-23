package store

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Default admin credentials for first boot (change via wizard).
const (
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "admin"
)

// Setting keys for setup wizard.
const (
	SettingSetupCompleted = "setup.completed"
	SettingListenAddr     = "listen.addr"
)

// User is a local admin account.
type User struct {
	ID           string `gorm:"primaryKey;size:36"`
	Username     string `gorm:"uniqueIndex;not null;size:64"`
	PasswordHash string `gorm:"not null;type:text"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EnsureDefaultAdmin creates admin:admin if no users exist.
func (db *DB) EnsureDefaultAdmin() error {
	var count int64
	if err := db.gorm.Model(&User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u := User{
		ID:           uuid.NewString(),
		Username:     DefaultAdminUsername,
		PasswordHash: string(hash),
	}
	return db.gorm.Create(&u).Error
}

// Authenticate verifies username/password. Returns user or error.
func (db *DB) Authenticate(username, password string) (*User, error) {
	var u User
	if err := db.gorm.Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("store: invalid credentials")
	}
	return &u, nil
}

// UpdatePassword sets a new password for username.
func (db *DB) UpdatePassword(username, newPassword string) error {
	if newPassword == "" {
		return errors.New("store: password required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res := db.gorm.Model(&User{}).Where("username = ?", username).Update("password_hash", string(hash))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// IsSetupComplete reports whether the wizard has been finished.
func (db *DB) IsSetupComplete() (bool, error) {
	v, err := db.GetSetting(SettingSetupCompleted)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return v == "true", nil
}

// MarkSetupComplete sets setup.completed=true.
func (db *DB) MarkSetupComplete() error {
	return db.SetSetting(SettingSetupCompleted, "true")
}

// SetupStatus describes wizard requirements.
type SetupStatus struct {
	WizardRequired   bool     `json:"wizard_required"`
	SetupComplete    bool     `json:"setup_complete"`
	HasSources       bool     `json:"has_sources"`
	HasLibraryMovies bool     `json:"has_library_movies"`
	HasLibraryTV     bool     `json:"has_library_tv"`
	DefaultUser      string   `json:"default_user,omitempty"`
	Missing          []string `json:"missing,omitempty"`
}

// GetSetupStatus evaluates whether the system needs the setup wizard.
// Wizard is required until setup.completed is true.
// Also reports missing recommended pieces (sources, library roots).
func (db *DB) GetSetupStatus() (*SetupStatus, error) {
	complete, err := db.IsSetupComplete()
	if err != nil {
		return nil, err
	}
	sources, err := db.ListSources()
	if err != nil {
		return nil, err
	}
	var movies, tv int64
	_ = db.gorm.Model(&LibraryRoot{}).Where("kind = ?", "movie").Count(&movies).Error
	_ = db.gorm.Model(&LibraryRoot{}).Where("kind = ?", "tv").Count(&tv).Error

	st := &SetupStatus{
		SetupComplete:    complete,
		WizardRequired:   !complete,
		HasSources:       len(sources) > 0,
		HasLibraryMovies: movies > 0,
		HasLibraryTV:     tv > 0,
	}
	if !complete {
		st.DefaultUser = DefaultAdminUsername
		if !st.HasSources {
			st.Missing = append(st.Missing, "source")
		}
		if !st.HasLibraryMovies {
			st.Missing = append(st.Missing, "library_movies")
		}
		if !st.HasLibraryTV {
			st.Missing = append(st.Missing, "library_tv")
		}
	}
	return st, nil
}
