package service

import (
	"errors"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func applyKKAIRiskMutations(tx *gorm.DB, input *normalizedRiskAction, result *RiskActionResult) ([]string, []string, error) {
	cooldownAllowed, _ := input.Metadata["client_token_cooldown_allowed"].(bool)
	if input.Source == RiskSourceUpstreamPolicy && cooldownAllowed {
		if err := validateRiskChannelIdentity(tx, input.ChannelID, input.UpstreamKeyFingerprint); err != nil {
			return nil, nil, err
		}
		if err := validateRiskTokenIdentity(tx, input.TokenID, input.UserID, input.TokenFingerprint); err != nil {
			return nil, nil, err
		}
		result.CooldownIdentityValidated = true
	}
	actions := make([]string, 0, 3)
	results := make([]string, 0, 3)
	if input.Actions.DisableToken {
		changed, err := disableRiskToken(tx, input.TokenID, input.UserID, input.TokenFingerprint)
		if err != nil {
			return nil, nil, err
		}
		result.TokenDisabled = changed
		actions = append(actions, "disable_token")
		results = append(results, changedResult(changed))
	}
	if input.Actions.DisableUser {
		changed, skipped, err := disableRiskUser(tx, input.UserID)
		if err != nil {
			return nil, nil, err
		}
		result.UserDisabled = changed
		result.UserDisableSkipped = skipped
		actions = append(actions, "disable_user")
		if skipped {
			results = append(results, "skipped_privileged")
		} else {
			results = append(results, changedResult(changed))
		}
	}
	if input.Actions.DisableChannel {
		changed, err := disableRiskChannel(tx, input.ChannelID, input.UpstreamKeyFingerprint)
		if err != nil {
			return nil, nil, err
		}
		result.ChannelDisabled = changed
		actions = append(actions, "disable_channel")
		results = append(results, changedResult(changed))
	}
	if len(actions) == 0 {
		return []string{"record_incident"}, []string{"recorded"}, nil
	}
	return actions, results, nil
}

func validateRiskTokenIdentity(tx *gorm.DB, tokenID int, userID int, fingerprint string) error {
	err := model.ValidateKKAIRiskToken(tx, tokenID, userID, fingerprint)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrRiskActionTokenNotFound
	}
	if errors.Is(err, model.ErrKKAIRiskFingerprintMismatch) {
		return ErrRiskActionIdentityMismatch
	}
	return err
}

func validateRiskChannelIdentity(tx *gorm.DB, channelID int, fingerprint string) error {
	err := model.ValidateKKAIRiskChannel(tx, channelID, fingerprint)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrRiskActionChannelNotFound
	}
	if errors.Is(err, model.ErrKKAIRiskFingerprintMismatch) {
		return ErrRiskActionIdentityMismatch
	}
	return err
}

func disableRiskToken(tx *gorm.DB, tokenID int, userID int, fingerprint string) (bool, error) {
	changed, err := model.DisableKKAIRiskToken(tx, tokenID, userID, fingerprint)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, ErrRiskActionTokenNotFound
	}
	if errors.Is(err, model.ErrKKAIRiskFingerprintMismatch) {
		return false, ErrRiskActionIdentityMismatch
	}
	return changed, err
}

func disableRiskUser(tx *gorm.DB, userID int) (bool, bool, error) {
	changed, skipped, err := model.DisableKKAIRiskUser(tx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, false, ErrRiskActionUserNotFound
	}
	return changed, skipped, err
}

func disableRiskChannel(tx *gorm.DB, channelID int, fingerprint string) (bool, error) {
	changed, err := model.DisableKKAIRiskChannel(tx, channelID, fingerprint)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, ErrRiskActionChannelNotFound
	}
	if errors.Is(err, model.ErrKKAIRiskFingerprintMismatch) {
		return false, ErrRiskActionIdentityMismatch
	}
	return changed, err
}

func changedResult(changed bool) string {
	if changed {
		return "disabled"
	}
	return "already_disabled"
}
