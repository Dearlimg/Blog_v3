package product

import "gorm.io/gorm"

type Service struct{ repo *Repository }

func NewService(db *gorm.DB) *Service { return &Service{repo: NewRepository(db)} }

func (s *Service) List(page, pageSize int, category, keyword string) ([]Entity, int64, error) {
	return s.repo.List(page, pageSize, category, keyword)
}

func (s *Service) ByID(id uint) (*Entity, error) { return s.repo.ByID(id) }

func (s *Service) Create(e *Entity) error { return s.repo.Create(e) }

func (s *Service) Update(id uint, updates map[string]any) error {
	e, err := s.repo.ByID(id)
	if err != nil {
		return err
	}
	return s.repo.Update(e, updates)
}

func (s *Service) Delete(id uint) error {
	e, err := s.repo.ByID(id)
	if err != nil {
		return err
	}
	return s.repo.Delete(e)
}
