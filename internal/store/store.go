package store

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM handle and domain operations.
type DB struct {
	gorm *gorm.DB
}

// Open opens (or creates) a SQLite database at path and migrates schema.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("store: empty database path")
	}
	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	db := &DB{gorm: gdb}
	if err := db.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.EnsureDefaultAdmin(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: default admin: %w", err)
	}
	return db, nil
}

func (db *DB) migrate() error {
	if err := db.gorm.AutoMigrate(
		&Source{},
		&Channel{},
		&Setting{},
		&LibraryRoot{},
		&CacheObject{},
		&User{},
	); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// Close releases the database.
func (db *DB) Close() error {
	if db == nil || db.gorm == nil {
		return nil
	}
	sqlDB, err := db.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GORM exposes the underlying handle for advanced queries (tests/admin).
func (db *DB) GORM() *gorm.DB {
	return db.gorm
}

// CreateSource inserts a source; generates ID if empty.
func (db *DB) CreateSource(s *Source) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.Name == "" {
		return errors.New("store: source name required")
	}
	if s.Type == "" {
		return errors.New("store: source type required")
	}
	return db.gorm.Create(s).Error
}

// GetSource loads a source by ID.
func (db *DB) GetSource(id string) (*Source, error) {
	var s Source
	if err := db.gorm.First(&s, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSources returns all sources ordered by priority desc, name.
func (db *DB) ListSources() ([]Source, error) {
	var list []Source
	err := db.gorm.Order("priority DESC, name ASC").Find(&list).Error
	return list, err
}

// ListSourcesByType filters by type.
func (db *DB) ListSourcesByType(typ string) ([]Source, error) {
	var list []Source
	err := db.gorm.Where("type = ?", typ).Order("priority DESC, name ASC").Find(&list).Error
	return list, err
}

// CreateChannel inserts a channel; generates ID if empty.
func (db *DB) CreateChannel(c *Channel) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.SourceID == "" || c.RemoteKey == "" || c.Name == "" {
		return errors.New("store: channel source_id, remote_key, and name required")
	}
	if c.Kind == "" {
		c.Kind = ChannelKindLive
	}
	return db.gorm.Create(c).Error
}

// ListChannelsBySource returns channels for a source.
func (db *DB) ListChannelsBySource(sourceID string) ([]Channel, error) {
	var list []Channel
	err := db.gorm.Where("source_id = ?", sourceID).Order("name ASC").Find(&list).Error
	return list, err
}

// DeleteChannelsBySource removes all channels for a source (refresh replace).
func (db *DB) DeleteChannelsBySource(sourceID string) error {
	return db.gorm.Where("source_id = ?", sourceID).Delete(&Channel{}).Error
}

// CreateChannels batch-inserts channels (IDs generated when empty).
func (db *DB) CreateChannels(chs []Channel) error {
	if len(chs) == 0 {
		return nil
	}
	for i := range chs {
		if chs[i].ID == "" {
			chs[i].ID = uuid.NewString()
		}
		if chs[i].Kind == "" {
			chs[i].Kind = ChannelKindLive
		}
	}
	return db.gorm.CreateInBatches(chs, 500).Error
}

// GetChannel loads a channel by ID.
func (db *DB) GetChannel(id string) (*Channel, error) {
	var c Channel
	if err := db.gorm.First(&c, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListChannels returns channels with optional filters (empty = all). Limit 0 = default 500.
func (db *DB) ListChannels(sourceID, kind, q string, limit, offset int) ([]Channel, error) {
	if limit <= 0 {
		limit = 500
	}
	tx := db.gorm.Model(&Channel{})
	if sourceID != "" {
		tx = tx.Where("source_id = ?", sourceID)
	}
	if kind != "" {
		tx = tx.Where("kind = ?", kind)
	}
	if q != "" {
		like := "%" + q + "%"
		tx = tx.Where("name LIKE ? OR group_name LIKE ?", like, like)
	}
	var list []Channel
	err := tx.Order("name ASC").Limit(limit).Offset(offset).Find(&list).Error
	return list, err
}

// CountChannels returns total channels, optionally by source.
func (db *DB) CountChannels(sourceID string) (int64, error) {
	tx := db.gorm.Model(&Channel{})
	if sourceID != "" {
		tx = tx.Where("source_id = ?", sourceID)
	}
	var n int64
	err := tx.Count(&n).Error
	return n, err
}

// SetSetting upserts a setting value.
func (db *DB) SetSetting(key, value string) error {
	if key == "" {
		return errors.New("store: setting key required")
	}
	s := Setting{Key: key, Value: value}
	return db.gorm.Save(&s).Error
}

// GetSetting returns a setting value or gorm.ErrRecordNotFound.
func (db *DB) GetSetting(key string) (string, error) {
	var s Setting
	if err := db.gorm.First(&s, "key = ?", key).Error; err != nil {
		return "", err
	}
	return s.Value, nil
}
