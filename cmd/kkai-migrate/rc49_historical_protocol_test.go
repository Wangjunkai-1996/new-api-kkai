//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireHistoricalMigrationRuntime(t *testing.T) {
	t.Helper()
	contract, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	require.NoError(t, err)
	if contract.RuntimeMinVersion != 14 || len(contract.RequiredExpansions) != 0 {
		t.Skip("historical v15-v21 positive protocol fixture requires its frozen compatibility artifact; rc49 deliberately rejects that mutation protocol")
	}
}

func TestRC49RejectsHistoricalMutationProofs(t *testing.T) {
	t.Run("reseller-prefix-v1", func(t *testing.T) {
		proof, status, now := resellerPrefixProofFixture(t)
		assert.Error(t, validateResellerPrefixProof(proof, status, now))
	})
	t.Run("request-ticket-v1", func(t *testing.T) {
		proof, status, now := requestTicketProofFixture(t)
		assert.Error(t, validateRequestTicketProof(proof, status, now))
	})
	t.Run("invoice-migration-v1", func(t *testing.T) {
		proof, status, now := invoiceProofFixture(t)
		assert.Error(t, validateInvoiceControllerProof(proof, status, now))
	})
	t.Run("wallet-payment-migration-v1", func(t *testing.T) {
		proof, status, now := walletPaymentProofFixture(t)
		assert.Error(t, validateWalletPaymentControllerProof(proof, status, now))
	})
	t.Run("shop-migration-v1", func(t *testing.T) {
		proof, status, now := shopProofFixture(t)
		assert.Error(t, validateShopControllerProof(proof, status, now))
	})
	t.Run("membership-payment-migration-v1", func(t *testing.T) {
		proof, status, now := membershipPaymentProofFixture(t)
		assert.Error(t, validateMembershipPaymentControllerProof(proof, status, now))
	})
	t.Run("membership-billing-recovery-migration-v1", func(t *testing.T) {
		proof, status, now := membershipBillingRecoveryProofFixture(t)
		assert.Error(t, validateMembershipBillingRecoveryControllerProof(proof, status, now))
	})
}
