package service

import (
	"context"
	"sort"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockVideoRowsForUpdate(query *gorm.DB) *gorm.DB {
	if query == nil || query.Dialector == nil || query.Dialector.Name() == "sqlite" {
		return query
	}
	return query.Clauses(clause.Locking{Strength: "UPDATE"})
}

func orderedVideoRowLockIDs(rowIDs []int64) []int64 {
	uniqueIDs := make(map[int64]struct{}, len(rowIDs))
	for _, rowID := range rowIDs {
		if rowID > 0 {
			uniqueIDs[rowID] = struct{}{}
		}
	}
	orderedIDs := make([]int64, 0, len(uniqueIDs))
	for rowID := range uniqueIDs {
		orderedIDs = append(orderedIDs, rowID)
	}
	sort.Slice(orderedIDs, func(i, j int) bool { return orderedIDs[i] < orderedIDs[j] })
	return orderedIDs
}

func lockVideoAssetRowsForUpdate(ctx context.Context, tx *gorm.DB, assetIDs []int64) ([]model.KKAIVideoAsset, error) {
	orderedIDs := orderedVideoRowLockIDs(assetIDs)
	if len(orderedIDs) == 0 {
		return []model.KKAIVideoAsset{}, nil
	}

	var assets []model.KKAIVideoAsset
	err := lockVideoRowsForUpdate(tx.WithContext(ctx)).
		Where("id IN ?", orderedIDs).
		Order("id ASC").
		Find(&assets).Error
	return assets, err
}

func lockVideoModelProfileRowsForUpdate(ctx context.Context, tx *gorm.DB, profileIDs []int64) ([]model.KKAIVideoModelProfile, error) {
	orderedIDs := orderedVideoRowLockIDs(profileIDs)
	if len(orderedIDs) == 0 {
		return []model.KKAIVideoModelProfile{}, nil
	}

	var profiles []model.KKAIVideoModelProfile
	err := lockVideoRowsForUpdate(tx.WithContext(ctx)).
		Where("id IN ?", orderedIDs).
		Order("id ASC").
		Find(&profiles).Error
	return profiles, err
}
