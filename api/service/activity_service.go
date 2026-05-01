package service

import (
	"context"
	"fmt"
	"time"

	"brc-connect-backend/api/repository"
)

type ActivityService struct {
	activityRepo *repository.ActivityRepo
	campaignRepo *repository.CampaignRepo
}

func NewActivityService(activityRepo *repository.ActivityRepo, campaignRepo *repository.CampaignRepo) *ActivityService {
	return &ActivityService{activityRepo: activityRepo, campaignRepo: campaignRepo}
}

func (s *ActivityService) GetFreshLeads(ctx context.Context, employeeID string, page int) ([]repository.CRMLeadView, int, error) {
	return s.activityRepo.GetFreshLeads(ctx, employeeID, page)
}

func (s *ActivityService) GetHistory(ctx context.Context, employeeID string, page, pageSize int) ([]repository.CRMLeadView, int, error) {
	return s.activityRepo.GetHistory(ctx, employeeID, page, pageSize)
}

func (s *ActivityService) UpdateActivity(ctx context.Context, activityID, employeeID string, updates map[string]any) error {
	// Verify the activity belongs to this employee
	activity, err := s.activityRepo.GetByID(ctx, activityID)
	if err != nil {
		return err
	}
	if activity == nil {
		return fmt.Errorf("activity not found")
	}
	if activity.EmployeeID != employeeID {
		return fmt.Errorf("access denied")
	}

	// Only allow safe fields
	allowed := map[string]bool{"status": true, "notes": true, "next_action": true, "next_follow_up": true, "last_contact": true}
	for k := range updates {
		if !allowed[k] {
			delete(updates, k)
		}
	}

	// Validate status if provided
	if status, ok := updates["status"].(string); ok {
		validStatuses := map[string]bool{
			"pending": true, "contacted": true, "follow_up": true,
			"converted": true, "not_interested": true, "closed": true,
		}
		if !validStatuses[status] {
			return fmt.Errorf("invalid status")
		}
		// Auto-set last_contact when status moves away from pending
		if status != "pending" {
			if _, hasLC := updates["last_contact"]; !hasLC {
				updates["last_contact"] = time.Now()
			}
		}
	}

	return s.activityRepo.UpdateActivity(ctx, activityID, updates)
}

func (s *ActivityService) GetDashboard(ctx context.Context, adminID string) ([]repository.EmployeeStats, error) {
	return s.activityRepo.GetDashboard(ctx, adminID)
}

func (s *ActivityService) GetEmployeeActivity(ctx context.Context, employeeID string, page, pageSize int) ([]repository.CRMLeadView, int, error) {
	return s.activityRepo.GetEmployeeActivity(ctx, employeeID, page, pageSize)
}

func (s *ActivityService) GetEmployeeStats(ctx context.Context, employeeID string) (*repository.EmployeeStats, error) {
	return s.activityRepo.GetEmployeeStats(ctx, employeeID)
}
