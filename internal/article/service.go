package article

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrConflict = errors.New("document changed; preserve both versions before resolving")
	ErrInvalid  = errors.New("invalid article request")
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type Service struct{ repo *Repository }

func NewService(db *gorm.DB) *Service { return &Service{repo: NewRepository(db)} }

func validate(m *Mutation) error {
	if !uuidPattern.MatchString(m.RequestID) {
		return ErrInvalid
	}
	tooLarge := len(m.Body) > 2*1024*1024 || len(m.PrivateNotes) > 128*1024
	badMetadata := len([]rune(m.Title)) > 200 || len([]rune(m.Summary)) > 1000 || len(m.Tags) > 12
	if tooLarge || badMetadata {
		return ErrInvalid
	}
	switch m.Category {
	case "", "interview", "fundamentals", "project", "notes":
	default:
		return ErrInvalid
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, tag := range m.Tags {
		tag = strings.TrimSpace(tag)
		if len([]rune(tag)) > 40 {
			return ErrInvalid
		}
		if tag != "" && !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	m.Tags = tags
	switch m.Action {
	case "save", "snapshot", "publish", "unpublish", "trash", "untrash", "restore":
	default:
		return ErrInvalid
	}
	if m.Action == "publish" && (strings.TrimSpace(m.Title) == "" || strings.TrimSpace(m.Body) == "") {
		return ErrInvalid
	}
	return nil
}

func (s *Service) Get(owner uint, id string) (*Document, error) {
	d, err := document(s.repo.db, id)
	if err != nil {
		return nil, err
	}
	if d.OwnerID != owner {
		return nil, gorm.ErrRecordNotFound
	}
	return d, nil
}

func snapshot(tx *gorm.DB, d *Draft, reason string) (*Revision, error) {
	r := Revision{ArticleID: d.ArticleID, Content: d.Content, Version: d.Version, Reason: reason}
	if err := tx.Create(&r).Error; err != nil {
		return nil, err
	}
	d.SnapshotAt = time.Now().UTC()
	if reason == "auto" {
		old := []uint{}
		if err := tx.Model(&Revision{}).Where("article_id = ? AND reason = ?", d.ArticleID, "auto").
			Order("id DESC").Offset(100).Limit(1000).Pluck("id", &old).Error; err != nil {
			return nil, err
		}
		if len(old) > 0 {
			if err := tx.Delete(&Revision{}, "id IN ?", old).Error; err != nil {
				return nil, err
			}
		}
	}
	return &r, nil
}

func (s *Service) Mutate(owner uint, id string, m Mutation) (*Document, error) {
	if !uuidPattern.MatchString(id) {
		return nil, ErrInvalid
	}
	if err := validate(&m); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(m)
	hash := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(hash[:])
	var result *Document
	err := s.repo.db.Transaction(func(tx *gorm.DB) error {
		// Lock the article before reading its draft or receipt; all mutations share this lock.
		locked := tx.Model(&Entity{}).Where("id = ? AND owner_id = ?", id, owner).
			UpdateColumn("updated_at", gorm.Expr("updated_at"))
		if locked.Error != nil {
			return locked.Error
		}
		d, err := document(tx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if m.Version != 0 || m.Action != "save" {
				return gorm.ErrRecordNotFound
			}
			d = &Document{Entity: Entity{ID: id, OwnerID: owner}, Draft: Draft{
				ArticleID: id, Content: Content{Tags: []string{}}, SnapshotAt: time.Now().UTC(),
			}}
			if err := tx.Create(&d.Entity).Error; err != nil {
				return err
			}
			if err := tx.Create(&d.Draft).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if d.OwnerID != owner {
			return gorm.ErrRecordNotFound
		}
		var receipt Receipt
		err = tx.First(&receipt, "article_id = ? AND request_id = ?", id, m.RequestID).Error
		if err == nil {
			if receipt.Fingerprint != fingerprint {
				return ErrConflict
			}
			return json.Unmarshal([]byte(receipt.Result), &result)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if d.Draft.Version != m.Version {
			return ErrConflict
		}
		if d.Trashed && m.Action != "untrash" {
			return ErrConflict
		}
		if err := applyMutation(tx, d, m); err != nil {
			return err
		}
		if err := tx.Save(&d.Draft).Error; err != nil {
			return err
		}
		if err := tx.Save(&d.Entity).Error; err != nil {
			return err
		}
		result = d
		// Save receipts need only the acknowledged version and publication state, not another full body copy.
		ack := *d
		if m.Action != "restore" {
			ack.Draft.Content = Content{Tags: []string{}}
		}
		payload, err := json.Marshal(ack)
		if err != nil {
			return err
		}
		return tx.Create(&Receipt{
			ArticleID: id, RequestID: m.RequestID, Fingerprint: fingerprint, Result: string(payload),
		}).Error
	})
	return result, err
}

func applyMutation(tx *gorm.DB, d *Document, m Mutation) error {
	if m.Action == "restore" {
		var r Revision
		if err := tx.First(&r, "id = ? AND article_id = ?", m.RevisionID, d.ID).Error; err != nil {
			return err
		}
		if _, err := snapshot(tx, &d.Draft, "before_restore"); err != nil {
			return err
		}
		d.Draft.Content = r.Content
	}
	d.Draft.Version++
	switch m.Action {
	case "save", "snapshot", "publish":
		d.Draft.Content = m.Content
	case "unpublish":
		d.PublishedRevisionID = 0
		d.PublishedAt = nil
	case "trash":
		d.Trashed = true
	case "untrash":
		d.Trashed = false
		d.PublishedRevisionID = 0
		d.PublishedAt = nil
	}
	reason := m.Action
	if reason == "save" {
		if time.Since(d.Draft.SnapshotAt) < 5*time.Minute {
			return nil
		}
		reason = "auto"
	}
	r, err := snapshot(tx, &d.Draft, reason)
	if err != nil {
		return err
	}
	if m.Action == "publish" {
		d.PublishedRevisionID = r.ID
		now := time.Now().UTC()
		d.PublishedAt = &now
	}
	return nil
}

func (s *Service) Revisions(owner uint, id string, page int) ([]Revision, int64, error) {
	if _, err := s.Get(owner, id); err != nil {
		return nil, 0, err
	}
	q := s.repo.db.Model(&Revision{}).Where("article_id = ?", id)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []Revision{}
	err := q.Omit("body", "private_notes").Order("id DESC").Limit(20).Offset((page - 1) * 20).Find(&items).Error
	return items, total, err
}
