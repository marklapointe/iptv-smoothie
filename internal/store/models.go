package store

import (
	"time"
)

// Source types supported by Smoothie.
const (
	SourceTypeIPTVM3U   = "iptv_m3u"
	SourceTypeHDHomeRun = "hdhomerun"
)

// Channel kinds.
const (
	ChannelKindLive = "live"
	ChannelKindVOD  = "vod"
)

// Source is an upstream feed (multiple of the same type allowed).
type Source struct {
	ID         string `gorm:"primaryKey;size:36"`
	Name       string `gorm:"not null;size:256"`
	Type       string `gorm:"not null;size:32;index"`
	Enabled    bool   `gorm:"not null;default:true"`
	Priority   int    `gorm:"not null;default:0"`
	ConfigJSON string `gorm:"type:text"`
	LimitsJSON string `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Channel is a streamable entry from a source.
type Channel struct {
	ID        string `gorm:"primaryKey;size:36"`
	SourceID  string `gorm:"not null;size:36;index;uniqueIndex:idx_source_remote"`
	RemoteKey string `gorm:"not null;size:512;uniqueIndex:idx_source_remote"`
	Name      string `gorm:"not null;size:512"`
	GroupName string `gorm:"size:256;index"`
	Kind      string `gorm:"not null;size:16;index"`
	StreamURL string `gorm:"type:text"`
	Logo      string `gorm:"type:text"`
	MetaJSON  string `gorm:"type:text"`
	Enabled   bool   `gorm:"not null;default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Setting is a key/value configuration row.
type Setting struct {
	Key       string `gorm:"primaryKey;size:128"`
	Value     string `gorm:"type:text"`
	UpdatedAt time.Time
}

// LibraryRoot maps movie vs TV filesystem roots.
type LibraryRoot struct {
	ID            string `gorm:"primaryKey;size:36"`
	Kind          string `gorm:"not null;size:16;uniqueIndex"` // movie | tv
	FSPath        string `gorm:"not null;type:text"`
	EmbyLibraryID string `gorm:"size:64"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CacheObject tracks progressive fill / purgatory / library media.
type CacheObject struct {
	ID            string `gorm:"primaryKey;size:36"`
	Key           string `gorm:"not null;uniqueIndex;size:512"`
	ChannelID     string `gorm:"size:36;index"`
	SourceID      string `gorm:"size:36;index"`
	Path          string `gorm:"type:text"`
	Zone          string `gorm:"not null;size:16;index"` // cache | purgatory | library
	SizeBytes     int64
	ExpectedSize  int64
	State         string `gorm:"not null;size:32;index"`
	Pin           bool   `gorm:"not null;default:false"`
	ETag          string `gorm:"size:256"`
	ContentHash   string `gorm:"size:128"`
	MediaKind     string `gorm:"size:16"` // movie | episode | other
	ShowName      string `gorm:"size:512"`
	Season        int
	Episode       int
	Year          int
	Title         string `gorm:"size:512"`
	LastAccessed  time.Time
	ValidatedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
