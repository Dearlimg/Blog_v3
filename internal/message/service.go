package message

import (
	"math/rand"

	"gorm.io/gorm"
)

var cuteNames = []string{
	"Sleepy Panda", "Happy Kitten", "Brave Bunny", "Shy Fox",
	"Curious Owl", "Lucky Duck", "Cozy Bear", "Gentle Deer",
	"Sunny Hamster", "Fluffy Sheep", "Jolly Penguin", "Tiny Star",
	"Little Cloud", "Peppy Squirrel", "Chirpy Bird", "Bubbly Fish",
	"Warm Mochi", "Dreamy Cat", "Bouncy Puppy", "Glowy Moon",
	"Sweet Mango", "Lazy Frog", "Perky Parrot", "Calm Turtle",
}

func pickCuteName() string {
	return cuteNames[rand.Intn(len(cuteNames))]
}

type Service struct{ repo *Repository }

func NewService(db *gorm.DB) *Service { return &Service{repo: NewRepository(db)} }

func (s *Service) List() ([]Entity, error) { return s.repo.List() }

func (s *Service) Create(req *CreateReq, ip string) (*Entity, error) {
	name := req.Name
	if name == "" {
		name = pickCuteName()
	}

	e := &Entity{
		Name:    name,
		Email:   req.Email,
		Content: req.Content,
		IP:      ip,
	}

	if err := s.repo.Create(e); err != nil {
		return nil, err
	}

	return e, nil
}
