package service

import (
	"context"
	"fmt"

	"github.com/shivanand-burli/go-starter-kit/password"

	"brc-connect-backend/api/models"
	"brc-connect-backend/api/repository"
)

type UserService struct {
	userRepo *repository.UserRepo
}

func NewUserService(userRepo *repository.UserRepo) *UserService {
	return &UserService{userRepo: userRepo}
}

func (s *UserService) Authenticate(ctx context.Context, email, pass string) (*models.User, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if !user.IsActive {
		return nil, fmt.Errorf("account deactivated")
	}
	if err := password.Verify(user.Password, pass); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return user, nil
}

func (s *UserService) CreateAdmin(ctx context.Context, name, email, pass string) (*models.User, error) {
	exists, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("email already in use")
	}

	hashed, err := password.Hash(pass)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := models.User{
		Name:     name,
		Email:    email,
		Password: hashed,
		Role:     "admin",
		IsActive: true,
	}

	id, err := s.userRepo.Insert(ctx, user)
	if err != nil {
		return nil, err
	}
	user.ID = id
	return &user, nil
}

func (s *UserService) CreateEmployee(ctx context.Context, adminID, name, email, pass string) (*models.User, error) {
	exists, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("email already in use")
	}

	hashed, err := password.Hash(pass)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := models.User{
		Name:     name,
		Email:    email,
		Password: hashed,
		Role:     "employee",
		AdminID:  &adminID,
		IsActive: true,
	}

	id, err := s.userRepo.Insert(ctx, user)
	if err != nil {
		return nil, err
	}
	user.ID = id
	return &user, nil
}

func (s *UserService) GetByID(ctx context.Context, id string) (*models.User, error) {
	return s.userRepo.GetByID(ctx, id)
}

func (s *UserService) GetAdmins(ctx context.Context, page, pageSize int) ([]models.User, int, error) {
	return s.userRepo.GetAdmins(ctx, page, pageSize)
}

func (s *UserService) GetEmployeesByAdmin(ctx context.Context, adminID string, page, pageSize int) ([]models.User, int, error) {
	return s.userRepo.GetEmployeesByAdmin(ctx, adminID, page, pageSize)
}

func (s *UserService) UpdateUser(ctx context.Context, id string, updates map[string]any) error {
	// If password is being updated, hash it
	if pass, ok := updates["password"].(string); ok && pass != "" {
		hashed, err := password.Hash(pass)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		updates["password"] = hashed
	}
	return s.userRepo.Update(ctx, id, updates)
}

func (s *UserService) DeactivateAdmin(ctx context.Context, adminID string) error {
	return s.userRepo.DeactivateWithEmployees(ctx, adminID)
}

// SeedSuperAdmin creates the super_admin if it doesn't exist. Called on startup.
func (s *UserService) SeedSuperAdmin(ctx context.Context, email, pass string) error {
	existing, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil // already seeded
	}

	hashed, err := password.Hash(pass)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	user := models.User{
		Name:     "Super Admin",
		Email:    email,
		Password: hashed,
		Role:     "super_admin",
		IsActive: true,
	}

	_, err = s.userRepo.Insert(ctx, user)
	return err
}
