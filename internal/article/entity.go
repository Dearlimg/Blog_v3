package article

import "time"

// Content is the complete editable document; private notes never enter PublicDocument.
type Content struct {
	Title        string   `json:"title" gorm:"size:200"`
	Summary      string   `json:"summary" gorm:"type:text"`
	Body         string   `json:"body" gorm:"type:longtext"`
	Category     string   `json:"category" gorm:"size:32;index"`
	Tags         []string `json:"tags" gorm:"serializer:json;type:text"`
	PrivateNotes string   `json:"private_notes" gorm:"type:longtext"`
}

type Entity struct {
	ID                  string     `json:"id" gorm:"primaryKey;size:36"`
	OwnerID             uint       `json:"-" gorm:"index;not null"`
	PublishedRevisionID uint       `json:"published_revision_id"`
	PublishedAt         *time.Time `json:"published_at"`
	Trashed             bool       `json:"trashed" gorm:"index"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (Entity) TableName() string { return "articles" }

type Draft struct {
	ArticleID  string `json:"article_id" gorm:"primaryKey;size:36"`
	Content    `gorm:"embedded"`
	Version    uint      `json:"version"`
	UpdatedAt  time.Time `json:"updated_at"`
	SnapshotAt time.Time `json:"-"`
}

func (Draft) TableName() string { return "article_drafts" }

type Revision struct {
	ID        uint   `json:"id" gorm:"primaryKey"`
	ArticleID string `json:"article_id" gorm:"size:36;index"`
	Content   `gorm:"embedded"`
	Version   uint      `json:"version"`
	Reason    string    `json:"reason" gorm:"size:32"`
	CreatedAt time.Time `json:"created_at"`
}

func (Revision) TableName() string { return "article_revisions" }

type Asset struct {
	ID        string `json:"id" gorm:"primaryKey;size:36"`
	ArticleID string `json:"article_id" gorm:"size:36;index"`
	MediaType string `json:"-" gorm:"size:32"`
	Data      []byte `json:"-" gorm:"type:mediumblob"`
}

func (Asset) TableName() string { return "article_assets" }

type Document struct {
	Entity
	Draft Draft `json:"draft"`
}

type PublicDocument struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Body        string     `json:"body,omitempty"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags" gorm:"serializer:json"`
	PublishedAt *time.Time `json:"published_at"`
}

type Mutation struct {
	Content
	Version    uint   `json:"version"`
	RequestID  string `json:"request_id"`
	Action     string `json:"action"`
	RevisionID uint   `json:"revision_id"`
}

// Receipts make retries safe even if the transaction committed but its response was lost.
type Receipt struct {
	ArticleID   string `gorm:"primaryKey;size:36"`
	RequestID   string `gorm:"primaryKey;size:36"`
	Fingerprint string `gorm:"size:64"`
	Result      string `gorm:"type:longtext"`
	CreatedAt   time.Time
}

func (Receipt) TableName() string { return "article_receipts" }
