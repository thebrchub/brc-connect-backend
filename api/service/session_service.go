package service

import (
	"context"

	"brc-connect-backend/api/repository"
)

type SessionService struct {
	sessionRepo *repository.SessionRepo
}

func NewSessionService(sessionRepo *repository.SessionRepo) *SessionService {
	return &SessionService{sessionRepo: sessionRepo}
}

func (s *SessionService) Heartbeat(ctx context.Context, employeeID string, sessionID *string, ip string) (string, error) {
	return s.sessionRepo.Heartbeat(ctx, employeeID, sessionID, ip)
}

func (s *SessionService) FlushSessions(ctx context.Context) error {
	return s.sessionRepo.FlushSessionsToDB(ctx)
}

func (s *SessionService) GetEngagement(ctx context.Context, employeeID string) (*repository.EngagementStats, error) {
	return s.sessionRepo.GetEngagement(ctx, employeeID)
}

func (s *SessionService) GetDashboardEngagement(ctx context.Context, adminID string) ([]repository.EngagementStats, error) {
	return s.sessionRepo.GetDashboardEngagement(ctx, adminID)
}
