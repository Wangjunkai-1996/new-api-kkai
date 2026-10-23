package main

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/require"
)

func TestInvoiceOrdinaryTargetCannotApply(t *testing.T) {
	// The normal CLI must reject a write before accessing any database, even
	// when a caller supplies Offline or a permissive library authorization.
	for _, options := range []kkaimigrate.Options{{}, {Offline: true}} {
		_, err := applyMigrationTarget(context.Background(), nil, kkaimigrate.InvoiceSchemaVersion, options)
		require.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
	}
}
