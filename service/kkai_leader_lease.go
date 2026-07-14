package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidLeaderLease  = errors.New("invalid KKAI leader lease configuration")
	leaderLeaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type KKAILeaderLeaseStore struct {
	db        *gorm.DB
	leaseName string
	now       func(context.Context) (int64, error)
}

func NewKKAILeaderLeaseStore(db *gorm.DB, leaseName string) *KKAILeaderLeaseStore {
	store := &KKAILeaderLeaseStore{db: db, leaseName: strings.TrimSpace(leaseName)}
	store.now = store.databaseUnixTime
	return store
}

func (s *KKAILeaderLeaseStore) TryAcquire(ctx context.Context, holder string, ttl time.Duration) (bool, error) {
	holder = strings.TrimSpace(holder)
	leaseUntil, now, err := s.deadline(ctx, holder, ttl)
	if err != nil {
		return false, err
	}
	candidate := model.KKAIJobLease{
		LeaseName:  s.leaseName,
		Holder:     holder,
		LeaseUntil: leaseUntil,
		Fence:      1,
		UpdatedAt:  now,
	}
	created := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate)
	if created.Error != nil {
		return false, created.Error
	}
	if created.RowsAffected == 1 {
		return true, nil
	}

	updated := s.db.WithContext(ctx).Model(&model.KKAIJobLease{}).
		Where("lease_name = ? AND lease_until <= ?", s.leaseName, now).
		Updates(map[string]any{
			"holder":      holder,
			"lease_until": leaseUntil,
			"fence":       gorm.Expr("fence + 1"),
			"updated_at":  now,
		})
	return updated.RowsAffected == 1, updated.Error
}

func (s *KKAILeaderLeaseStore) Renew(ctx context.Context, holder string, ttl time.Duration) (bool, error) {
	holder = strings.TrimSpace(holder)
	leaseUntil, now, err := s.deadline(ctx, holder, ttl)
	if err != nil {
		return false, err
	}
	updated := s.db.WithContext(ctx).Model(&model.KKAIJobLease{}).
		Where("lease_name = ? AND holder = ? AND lease_until > ?", s.leaseName, holder, now).
		Updates(map[string]any{"lease_until": leaseUntil, "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

func (s *KKAILeaderLeaseStore) Release(ctx context.Context, holder string) error {
	holder = strings.TrimSpace(holder)
	if !s.configured() || !leaderLeaseNamePattern.MatchString(holder) {
		return ErrInvalidLeaderLease
	}
	now, err := s.now(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&model.KKAIJobLease{}).
		Where("lease_name = ? AND holder = ?", s.leaseName, holder).
		Updates(map[string]any{"holder": "", "lease_until": 0, "updated_at": now}).Error
}

func (s *KKAILeaderLeaseStore) Current(ctx context.Context) (*model.KKAIJobLease, error) {
	if !s.configured() {
		return nil, ErrInvalidLeaderLease
	}
	var lease model.KKAIJobLease
	if err := s.db.WithContext(ctx).Where("lease_name = ?", s.leaseName).First(&lease).Error; err != nil {
		return nil, err
	}
	return &lease, nil
}

func (s *KKAILeaderLeaseStore) deadline(ctx context.Context, holder string, ttl time.Duration) (int64, int64, error) {
	if !s.configured() || !leaderLeaseNamePattern.MatchString(holder) || ttl <= 0 {
		return 0, 0, ErrInvalidLeaderLease
	}
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		seconds++
	}
	if seconds <= 0 {
		return 0, 0, ErrInvalidLeaderLease
	}
	now, err := s.now(ctx)
	if err != nil || now <= 0 {
		return 0, 0, fmt.Errorf("%w: database time: %v", ErrInvalidLeaderLease, err)
	}
	return now + seconds, now, nil
}

func (s *KKAILeaderLeaseStore) configured() bool {
	return s != nil && s.db != nil && s.now != nil && leaderLeaseNamePattern.MatchString(s.leaseName)
}

func (s *KKAILeaderLeaseStore) databaseUnixTime(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrInvalidLeaderLease
	}
	var query string
	switch s.db.Dialector.Name() {
	case "postgres":
		query = "SELECT EXTRACT(EPOCH FROM CURRENT_TIMESTAMP)::bigint"
	case "mysql":
		query = "SELECT UNIX_TIMESTAMP()"
	case "sqlite":
		query = "SELECT CAST(strftime('%s','now') AS INTEGER)"
	default:
		return 0, fmt.Errorf("%w: unsupported database dialect", ErrInvalidLeaderLease)
	}
	var now int64
	if err := s.db.WithContext(ctx).Raw(query).Scan(&now).Error; err != nil {
		return 0, err
	}
	if now <= 0 {
		return 0, ErrInvalidLeaderLease
	}
	return now, nil
}
