package main

import (
	"context"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMembershipPaymentOrdinaryTargetCannotMutate(t *testing.T) {
	for _, options := range []kkaimigrate.Options{{}, {Offline: true}} {
		_, err := applyMigrationTarget(context.Background(), nil, kkaimigrate.MembershipPaymentSchemaVersion, options)
		assert.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
	}
}
