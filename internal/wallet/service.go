package wallet

import (
	"fmt"

	"gorm.io/gorm"
)

type Service struct{ repo *Repository }

func NewService(db *gorm.DB) *Service { return &Service{repo: NewRepository(db)} }

func (s *Service) GetOrCreate(userID uint) (*Entity, error) {
	e, err := s.repo.ByUserID(userID)
	if err == nil {
		return e, nil
	}

	e = &Entity{UserID: userID, Balance: 0}
	if err := s.repo.CreateIfNotExists(e); err != nil {
		return nil, err
	}

	return s.repo.ByUserID(userID)
}

func (s *Service) AddBalance(userID uint, req *AddBalanceReq) (*Entity, *Transaction, error) {
	e, err := s.repo.ByUserID(userID)
	if err != nil {
		return nil, nil, err
	}

	e.Balance += req.Amount

	desc := req.Description
	if desc == "" {
		desc = "balance top-up"
	}

	txn := Transaction{WalletID: e.ID, Amount: req.Amount, Type: "income", Description: desc}

	if err := s.AddTransactions(e, txn); err != nil {
		return nil, nil, err
	}

	return e, &txn, nil
}

func (s *Service) Transfer(userID uint, req *TransferReq) (*Entity, *Entity, []Transaction, error) {
	if userID == req.ToUserID {
		return nil, nil, nil, ErrSelfTransfer
	}

	var from, to *Entity
	var txns []Transaction

	err := s.repo.Transaction(func(tx *gorm.DB) error {
		var err error
		from, err = s.repo.ByUserID(userID)
		if err != nil {
			return err
		}

		if from.Balance < req.Amount {
			return ErrBalance
		}

		to, err = s.repo.ByUserID(req.ToUserID)
		if err != nil {
			return err
		}

		from.Balance -= req.Amount
		to.Balance += req.Amount

		tx.Save(from)
		tx.Save(to)

		outDesc := req.Description
		if outDesc == "" {
			outDesc = fmt.Sprintf("transfer to user %d", req.ToUserID)
		}

		out := Transaction{WalletID: from.ID, Amount: -req.Amount, Type: "transfer_out", Description: outDesc, RelatedID: &to.ID}
		in := Transaction{WalletID: to.ID, Amount: req.Amount, Type: "transfer_in", Description: fmt.Sprintf("transfer from user %d", userID), RelatedID: &from.ID}

		txns = []Transaction{out, in}
		return tx.Create(&txns).Error
	})
	if err != nil {
		return nil, nil, nil, err
	}

	return from, to, txns, nil
}

func (s *Service) Transactions(userID uint) ([]Transaction, error) {
	e, err := s.repo.ByUserID(userID)
	if err != nil {
		return nil, err
	}

	return s.repo.Transactions(e.ID)
}

func (s *Service) AddTransactions(e *Entity, txns ...Transaction) error {
	return s.repo.Transaction(func(tx *gorm.DB) error {
		tx.Save(e)
		return tx.Create(&txns).Error
	})
}

var (
	ErrSelfTransfer = errSelfTransfer{}
	ErrBalance      = errBalance{}
)

type errSelfTransfer struct{}

func (e errSelfTransfer) Error() string { return "cannot transfer to yourself" }

type errBalance struct{}

func (e errBalance) Error() string { return "insufficient balance" }
