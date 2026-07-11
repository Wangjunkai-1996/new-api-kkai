package model

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type policyIncidentClientDisableResult struct {
	Token          *Token
	TokenChanged   bool
	TokenError     error
	User           *User
	UserChanged    bool
	UserPrivileged bool
	UserError      error
}

func PersistPolicyIncidentClientDisable(event *PolicyIncidentEvent, tokenId int, userId int) error {
	if event == nil {
		return ErrNilPolicyIncidentEvent
	}

	var result policyIncidentClientDisableResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		result.Token, result.TokenChanged, result.TokenError = disableTokenByIds(tx, tokenId, userId)
		if result.TokenError != nil {
			return fmt.Errorf("disable policy incident token: %w", result.TokenError)
		}
		result.User, result.UserChanged, result.UserError = disableUserForPolicyIncident(tx, userId)
		result.UserPrivileged = errors.Is(result.UserError, ErrPolicyIncidentPrivilegedUser)
		if result.UserError != nil && !result.UserPrivileged {
			return fmt.Errorf("disable policy incident user: %w", result.UserError)
		}
		appendPolicyIncidentClientDisableAudit(event, result)
		return tx.Create(event).Error
	})
	if err != nil {
		return err
	}

	if result.Token != nil && result.TokenError == nil {
		cacheTokenAsync(*result.Token)
	}
	if result.User != nil && result.UserError == nil {
		invalidatePolicyIncidentUserCaches(userId)
	}
	return nil
}

func appendPolicyIncidentClientDisableAudit(event *PolicyIncidentEvent, result policyIncidentClientDisableResult) {
	tokenAction, tokenResult := "token_unchanged", "already_disabled"
	if result.TokenChanged {
		tokenAction = "token_disabled"
		tokenResult = "success"
	}

	userAction, userResult := "user_unchanged", "already_disabled"
	if result.UserPrivileged {
		userAction = "user_disable_skipped_privileged"
		userResult = "privileged_user"
	} else if result.UserChanged {
		userAction = "user_disabled"
		userResult = "success"
	}

	event.ActionTaken = appendPolicyIncidentAuditValue(event.ActionTaken, tokenAction, userAction)
	event.ActionResult = appendPolicyIncidentAuditValue(event.ActionResult, tokenResult, userResult)
}

func appendPolicyIncidentAuditValue(current string, values ...string) string {
	parts := make([]string, 0, len(values)+1)
	if strings.TrimSpace(current) != "" {
		parts = append(parts, current)
	}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ",")
}
