package main

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
)

func TestRequestTicketTargetNeverExposesUnguardedMutation(t *testing.T) {
	_, err := applyMigrationTarget(context.Background(), nil, 16, kkaimigrate.Options{})
	assert.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
}
