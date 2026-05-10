package comment

import (
	"blog-front/internal/user"

	"gorm.io/gorm"
)

type Service struct {
	repo     *Repository
	userRepo *user.Repository
}

func NewService(db *gorm.DB) *Service {
	return &Service{repo: NewRepository(db), userRepo: user.NewRepository(db)}
}

func (s *Service) List(page, pageSize int) ([]Entity, int64, error) {
	list, total, err := s.repo.ListRoot(page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	for i := range list {
		s.repo.FillUsername(&list[i])
		for j := range list[i].Replies {
			s.repo.FillUsername(&list[i].Replies[j])
		}
	}

	return list, total, nil
}

func (s *Service) Create(req *CreateReq) (*Entity, error) {
	u, err := s.userRepo.ByID(req.UserID)
	if err != nil {
		return nil, err
	}

	e := Entity{
		UserID:   req.UserID,
		Content:  req.Content,
		ParentID: req.ParentID,
	}
	if err := s.repo.Create(&e); err != nil {
		return nil, err
	}

	e.Username = u.Username
	return &e, nil
}

func (s *Service) Update(userID, id uint, req *UpdateReq) (*Entity, error) {
	e, err := s.repo.ByID(id)
	if err != nil {
		return nil, err
	}

	if e.UserID != userID {
		return nil, ErrForbidden
	}

	e.Content = req.Content
	if err := s.repo.Save(e); err != nil {
		return nil, err
	}

	return e, nil
}

func (s *Service) Delete(userID, id uint) error {
	e, err := s.repo.ByID(id)
	if err != nil {
		return err
	}

	if e.UserID != userID {
		return ErrForbidden
	}

	return s.repo.Delete(e)
}

var ErrForbidden = errForbidden{}

type errForbidden struct{}

func (e errForbidden) Error() string { return "forbidden" }
