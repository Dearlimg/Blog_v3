package order

import (
	"fmt"

	"blog-front/internal/product"

	"gorm.io/gorm"
)

type Service struct {
	repo        *Repository
	productRepo *product.Repository
}

func NewService(db *gorm.DB) *Service {
	return &Service{repo: NewRepository(db), productRepo: product.NewRepository(db)}
}

// ---- Order ----

func (s *Service) CreateOrder(userID uint, req *CreateReq) (*Entity, error) {
	p, err := s.productRepo.ByID(req.ProductID)
	if err != nil {
		return nil, err
	}

	if p.Stock < req.Quantity {
		return nil, ErrStock
	}

	total := p.Price * float64(req.Quantity)
	o := &Entity{
		UserID: userID, ProductID: req.ProductID,
		Quantity: req.Quantity, Address: req.Address,
		Total: total, Status: "pending",
	}

	err = s.repo.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateOrder(o, tx); err != nil {
			return err
		}
		return s.repo.DeductStock(p, req.Quantity, tx)
	})
	if err != nil {
		return nil, err
	}

	o.Product = *p
	return o, nil
}

func (s *Service) ListOrders(userID uint, page, pageSize int) ([]Entity, int64, error) {
	return s.repo.ListOrders(userID, page, pageSize)
}

func (s *Service) OrderByID(userID, id uint) (*Entity, error) {
	return s.repo.OrderByID(userID, id)
}

// ---- Cart ----

func (s *Service) CartItems(userID uint) ([]CartItem, error) {
	return s.repo.CartItems(userID)
}

func (s *Service) AddCartItem(userID uint, req *AddCartReq) (*CartItem, error) {
	p, err := s.productRepo.ByID(req.ProductID)
	if err != nil {
		return nil, err
	}

	if existing, err := s.repo.CartItemByProduct(userID, req.ProductID); err == nil {
		existing.Quantity += req.Quantity
		if err := s.repo.SaveCartItem(existing); err != nil {
			return nil, err
		}
		existing.Product = *p
		return existing, nil
	}

	item := &CartItem{UserID: userID, ProductID: req.ProductID, Quantity: req.Quantity}
	if err := s.repo.CreateCartItem(item); err != nil {
		return nil, err
	}

	item.Product = *p
	return item, nil
}

func (s *Service) UpdateCartItem(userID, itemID uint, req *UpdateCartReq) (*CartItem, error) {
	item, err := s.repo.CartItemByID(itemID)
	if err != nil {
		return nil, err
	}

	if item.UserID != userID {
		return nil, ErrForbidden
	}

	item.Quantity = req.Quantity
	if err := s.repo.SaveCartItem(item); err != nil {
		return nil, err
	}

	p, _ := s.productRepo.ByID(item.ProductID)
	item.Product = *p
	return item, nil
}

func (s *Service) RemoveCartItem(userID, itemID uint) error {
	item, err := s.repo.CartItemByID(itemID)
	if err != nil {
		return err
	}

	if item.UserID != userID {
		return ErrForbidden
	}

	return s.repo.DeleteCartItem(item)
}

func (s *Service) Checkout(userID uint, req *CheckoutReq) ([]Entity, float64, error) {
	items, err := s.repo.CartItems(userID)
	if err != nil {
		return nil, 0, err
	}

	if len(items) == 0 {
		return nil, 0, ErrEmptyCart
	}

	var total float64
	var orders []Entity

	err = s.repo.Transaction(func(tx *gorm.DB) error {
		for _, item := range items {
			p := item.Product
			if p.Stock < item.Quantity {
				return fmt.Errorf("insufficient stock for %s", p.Name)
			}

			subTotal := p.Price * float64(item.Quantity)
			total += subTotal

			o := Entity{
				UserID: userID, ProductID: item.ProductID,
				Quantity: item.Quantity, Address: req.Address,
				Total: subTotal, Status: "pending",
			}

			if err := tx.Create(&o).Error; err != nil {
				return err
			}

			if err := tx.Model(&p).Update("stock", gorm.Expr("stock - ?", item.Quantity)).Error; err != nil {
				return err
			}

			o.Product = p
			orders = append(orders, o)
		}

		return tx.Where("user_id = ?", userID).Delete(&CartItem{}).Error
	})
	if err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

var (
	ErrStock     = errStock{}
	ErrForbidden = errForbidden{}
	ErrEmptyCart = errEmptyCart{}
)

type errStock struct{}

func (e errStock) Error() string { return "insufficient stock" }

type errForbidden struct{}

func (e errForbidden) Error() string { return "forbidden" }

type errEmptyCart struct{}

func (e errEmptyCart) Error() string { return "cart is empty" }
