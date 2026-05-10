package message

import (
	"gorm.io/gorm"
)

type Service struct{ repo *Repository }

func NewService(db *gorm.DB) *Service { return &Service{repo: NewRepository(db)} }

func (s *Service) List() ([]Entity, error) { return s.repo.List() }

func (s *Service) Create(req *CreateReq) (*Entity, error) {
	e := &Entity{Name: req.Name, Email: req.Email, Content: req.Content}
	if err := s.repo.Create(e); err != nil {
		return nil, err
	}
	return e, nil
}
