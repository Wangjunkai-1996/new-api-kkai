package main

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMembershipBillingRecoveryOrdinaryTargetCannotMutate(t *testing.T) {
	for _, options := range []kkaimigrate.Options{{}, {Offline: true}, {AuthorizeMutation: func(context.Context, *gorm.DB) error { return nil }}} {
		_, err := applyMigrationTarget(context.Background(), nil, kkaimigrate.MembershipBillingRecoverySchemaVersion, options)
		assert.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
	}
}
