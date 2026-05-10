package user

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Service struct {
	repo      *Repository
	jwtSecret string
	jwtExpire int
}

func NewService(db *gorm.DB, jwtSecret string, jwtExpire int) *Service {
	return &Service{repo: NewRepository(db), jwtSecret: jwtSecret, jwtExpire: jwtExpire}
}

func (s *Service) Register(req *RegisterReq) (*Entity, error) {
	if _, err := s.repo.ByEmail(req.Email); err == nil {
		return nil, ErrDuplicateEmail
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	e := &Entity{
		Username: req.Username,
		Email:    req.Email,
		Password: string(hashed),
	}

	if err := s.repo.Create(e); err != nil {
		return nil, err
	}

	return e, nil
}

func (s *Service) Login(req *LoginReq) (*LoginResp, error) {
	u, err := s.repo.ByEmail(req.Email)
	if err != nil {
		return nil, ErrInvalidCred
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCred
	}

	token, err := s.generateToken(u.ID)
	if err != nil {
		return nil, err
	}

	return &LoginResp{Token: token, User: u}, nil
}

func (s *Service) SendCode(email string) error {
	if _, err := s.repo.ByEmail(email); err != nil {
		return ErrNotFound
	}
	return nil
}

func (s *Service) VerifyEmail(email, code string) error {
	u, err := s.repo.ByEmail(email)
	if err != nil {
		return ErrNotFound
	}

	u.Verified = true
	return s.repo.Save(u)
}

func (s *Service) Profile(userID uint) (*Entity, error) {
	return s.repo.ByID(userID)
}

func (s *Service) generateToken(userID uint) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(time.Duration(s.jwtExpire) * time.Second).Unix(),
		"iat":     time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

var (
	ErrDuplicateEmail = errDuplicateEmail{}
	ErrInvalidCred    = errInvalidCred{}
	ErrNotFound       = errNotFound{}
)

type errDuplicateEmail struct{}

func (e errDuplicateEmail) Error() string { return "email already registered" }

type errInvalidCred struct{}

func (e errInvalidCred) Error() string { return "invalid email or password" }

type errNotFound struct{}

func (e errNotFound) Error() string { return "not found" }
