package article

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

type Filter struct {
	Page     int
	Category string
	Tag      string
	Keyword  string
	State    string
}

type SitemapEntry struct {
	ID          string
	PublishedAt *time.Time
}

func literalLike(value string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value)
}

func (r *Repository) PublicList(f Filter) ([]PublicDocument, int64, error) {
	q := r.db.Table("articles AS a").Joins("JOIN article_revisions AS r ON r.id = a.published_revision_id").
		Where("a.trashed = ?", false)
	if f.Category != "" {
		q = q.Where("r.category = ?", f.Category)
	}
	if f.Tag != "" {
		tag, _ := json.Marshal(f.Tag)
		q = q.Where("r.tags LIKE ? ESCAPE '!'", "%"+literalLike(string(tag))+"%")
	}
	if f.Keyword != "" {
		pattern := "%" + literalLike(f.Keyword) + "%"
		q = q.Where("(r.title LIKE ? ESCAPE '!' OR r.summary LIKE ? ESCAPE '!' OR r.body LIKE ? ESCAPE '!')",
			pattern, pattern, pattern)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []PublicDocument{}
	err := q.Select("a.id, r.title, r.summary, r.category, r.tags, a.published_at").
		Order("a.published_at DESC, a.id").Limit(12).Offset((f.Page - 1) * 12).Find(&items).Error
	return items, total, err
}

func (r *Repository) PublicGet(id string) (*PublicDocument, error) {
	var item PublicDocument
	err := r.db.Table("articles AS a").Joins("JOIN article_revisions AS r ON r.id = a.published_revision_id").
		Select("a.id, r.title, r.summary, r.body, r.category, r.tags, a.published_at").
		Where("a.id = ? AND a.trashed = ?", id, false).Take(&item).Error
	return &item, err
}

func (r *Repository) PublicSitemapEntries() ([]SitemapEntry, error) {
	entries := []SitemapEntry{}
	err := r.db.Table("articles AS a").
		Joins("JOIN article_revisions AS r ON r.id = a.published_revision_id").
		Select("a.id, a.published_at").
		Where("a.trashed = ? AND a.published_revision_id <> ?", false, 0).
		Order("a.published_at DESC, a.id").
		Find(&entries).Error
	return entries, err
}

func document(db *gorm.DB, id string) (*Document, error) {
	var d Document
	if err := db.First(&d.Entity, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if err := db.First(&d.Draft, "article_id = ?", id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repository) List(owner uint, f Filter) ([]Document, int64, error) {
	q := r.db.Model(&Entity{}).Where("owner_id = ? AND trashed = ?", owner, f.State == "trash")
	switch f.State {
	case "published":
		q = q.Where("published_revision_id <> 0")
	case "draft":
		q = q.Where("published_revision_id = 0")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []Entity{}
	if err := q.Order("updated_at DESC, id").Limit(12).Offset((f.Page - 1) * 12).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	result := []Document{}
	for _, item := range items {
		d, err := document(r.db, item.ID)
		if err != nil {
			return nil, 0, err
		}
		d.Draft.Body = ""
		d.Draft.PrivateNotes = ""
		result = append(result, *d)
	}
	return result, total, nil
}
