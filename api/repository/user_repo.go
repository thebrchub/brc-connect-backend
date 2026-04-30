package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/models"
)

type UserRepo struct {
	userTTL time.Duration
	listTTL time.Duration
}

func NewUserRepo(userTTL, listTTL time.Duration) *UserRepo {
	return &UserRepo{userTTL: userTTL, listTTL: listTTL}
}

func (r *UserRepo) Insert(ctx context.Context, u models.User) (string, error) {
	u.ID = uuid.NewString()
	_, err := postgress.Exec(ctx,
		`INSERT INTO users (id, name, email, password, role, admin_id, is_active, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,NOW(),NOW())`,
		u.ID, u.Name, u.Email, u.Password, u.Role, u.AdminID, u.IsActive)
	if err != nil {
		return "", err
	}
	r.invalidateListCache(ctx, u.Role, u.AdminID)
	return u.ID, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*models.User, error) {
	user, err := redis.Fetch(ctx, "user:"+id, r.userTTL, func(ctx context.Context) (*models.User, error) {
		var u models.User
		found, err := postgress.Get(ctx, "users", id, &u)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		return &u, nil
	})
	return user, err
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	rows, err := postgress.Query[models.User](ctx,
		"SELECT * FROM users WHERE email = $1 LIMIT 1", email)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *UserRepo) GetAdmins(ctx context.Context, page, pageSize int) ([]models.User, int, error) {
	cacheKey := fmt.Sprintf("users:admins:list:%d:%d", page, pageSize)

	type listResult struct {
		Users []models.User `json:"users"`
		Total int           `json:"total"`
	}

	result, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*listResult, error) {
		rows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, "SELECT COUNT(*) AS count FROM users WHERE role = 'admin'")
		if err != nil {
			return nil, err
		}
		total := 0
		if len(rows) > 0 {
			total = rows[0].Count
		}

		offset := (page - 1) * pageSize
		users, err := postgress.Query[models.User](ctx,
			fmt.Sprintf("SELECT * FROM users WHERE role = 'admin' ORDER BY created_at DESC LIMIT %d OFFSET %d", pageSize, offset))
		if err != nil {
			return nil, err
		}
		return &listResult{Users: users, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	return result.Users, result.Total, nil
}

func (r *UserRepo) GetEmployeesByAdmin(ctx context.Context, adminID string, page, pageSize int) ([]models.User, int, error) {
	cacheKey := fmt.Sprintf("users:employees:%s:%d:%d", adminID, page, pageSize)

	type listResult struct {
		Users []models.User `json:"users"`
		Total int           `json:"total"`
	}

	result, err := redis.Fetch(ctx, cacheKey, r.listTTL, func(ctx context.Context) (*listResult, error) {
		rows, err := postgress.Query[struct {
			Count int `db:"count"`
		}](ctx, "SELECT COUNT(*) AS count FROM users WHERE admin_id = $1 AND role = 'employee'", adminID)
		if err != nil {
			return nil, err
		}
		total := 0
		if len(rows) > 0 {
			total = rows[0].Count
		}

		offset := (page - 1) * pageSize
		users, err := postgress.Query[models.User](ctx,
			"SELECT * FROM users WHERE admin_id = $1 AND role = 'employee' ORDER BY created_at DESC LIMIT $2 OFFSET $3",
			adminID, pageSize, offset)
		if err != nil {
			return nil, err
		}
		return &listResult{Users: users, Total: total}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	return result.Users, result.Total, nil
}

func (r *UserRepo) Update(ctx context.Context, id string, updates map[string]any) error {
	updates["updated_at"] = time.Now()

	setClauses := ""
	args := []any{}
	argIdx := 1
	for k, v := range updates {
		if setClauses != "" {
			setClauses += ", "
		}
		setClauses += fmt.Sprintf("%s = $%d", k, argIdx)
		args = append(args, v)
		argIdx++
	}
	args = append(args, id)

	_, err := postgress.Exec(ctx, fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", setClauses, argIdx), args...)
	if err != nil {
		return err
	}
	redis.Remove(ctx, "user:"+id)
	r.invalidateByPattern(ctx, "users:*")
	return nil
}

func (r *UserRepo) DeactivateWithEmployees(ctx context.Context, adminID string) error {
	_, err := postgress.Exec(ctx,
		"UPDATE users SET is_active = false, updated_at = NOW() WHERE admin_id = $1 AND role = 'employee'", adminID)
	if err != nil {
		return err
	}
	_, err = postgress.Exec(ctx,
		"UPDATE users SET is_active = false, updated_at = NOW() WHERE id = $1", adminID)
	if err != nil {
		return err
	}
	r.invalidateByPattern(ctx, "users:*")
	redis.Remove(ctx, "user:"+adminID)
	return nil
}

func (r *UserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	rows, err := postgress.Query[struct {
		Exists bool `db:"exists"`
	}](ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email = $1) AS exists", email)
	if err != nil {
		return false, err
	}
	return len(rows) > 0 && rows[0].Exists, nil
}

func (r *UserRepo) invalidateListCache(ctx context.Context, role string, adminID *string) {
	if role == "admin" {
		r.invalidateByPattern(ctx, "users:admins:*")
	}
	if role == "employee" && adminID != nil {
		r.invalidateByPattern(ctx, fmt.Sprintf("users:employees:%s:*", *adminID))
	}
}

func (r *UserRepo) invalidateByPattern(ctx context.Context, pattern string) {
	client := redis.GetRawClient()
	iter := client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		client.Del(ctx, iter.Val())
	}
}
