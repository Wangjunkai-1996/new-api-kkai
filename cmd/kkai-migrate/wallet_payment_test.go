package main

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/require"
)

func TestWalletPaymentOrdinaryTargetCannotMutate(t *testing.T) {
	for _, options := range []kkaimigrate.Options{{}, {Offline: true}} {
		_, err := applyMigrationTarget(context.Background(), nil, kkaimigrate.WalletPaymentSchemaVersion, options)
		require.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
	}
}
