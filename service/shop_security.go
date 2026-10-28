package service

import (
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	VerificationScopeShopInventoryReveal  = "shop.inventory.reveal"
	VerificationScopeShopDeliveryCreate   = "shop.delivery.create"
	VerificationScopeShopDeliveryReveal   = "shop.delivery.reveal"
	VerificationScopeShopRiskRelease      = "shop.risk.release"
	VerificationScopeShopRefundManage     = "shop.refund.manage"
	VerificationScopeShopAdjustmentManage = "shop.payment.adjustment.manage"
	VerificationScopeShopEvidenceCreate   = "shop.payment.evidence.create"
)

type ShopProofMoney struct {
	AmountMinor string `json:"amount_minor"`
	Currency    string `json:"currency"`
	Exponent    int    `json:"exponent"`
}

type ShopVerificationContext struct {
	Action          string                  `json:"action"`
	TargetID        string                  `json:"target_id"`
	ExpectedVersion string                  `json:"expected_version"`
	IdempotencyKey  string                  `json:"idempotency_key,omitempty"`
	Details         ShopVerificationDetails `json:"details"`
}

// The explicit fields are shared by issuance and consumption. Secret payloads
// deliberately have no representation in this authorization context.
type ShopVerificationDetails struct {
	Reason                       string          `json:"reason,omitempty"`
	TemplateVersion              string          `json:"template_version,omitempty"`
	CaseID                       string          `json:"case_id,omitempty"`
	CaseVersion                  string          `json:"case_version,omitempty"`
	DeliveryID                   string          `json:"delivery_id,omitempty"`
	AttemptID                    string          `json:"attempt_id,omitempty"`
	AdjustmentID                 string          `json:"adjustment_id,omitempty"`
	Kind                         string          `json:"kind,omitempty"`
	Money                        *ShopProofMoney `json:"money,omitempty"`
	EvidenceIDs                  []string        `json:"evidence_ids,omitempty"`
	PublicNote                   string          `json:"public_note,omitempty"`
	ChannelRef                   string          `json:"channel_ref,omitempty"`
	OperatedAt                   string          `json:"operated_at,omitempty"`
	Conclusion                   string          `json:"conclusion,omitempty"`
	ProviderAdjustmentRef        string          `json:"provider_adjustment_ref,omitempty"`
	State                        string          `json:"state,omitempty"`
	Source                       string          `json:"source,omitempty"`
	ProviderTransactionID        string          `json:"provider_transaction_id,omitempty"`
	ProviderRefundOrAdjustmentID string          `json:"provider_refund_or_adjustment_id,omitempty"`
	ObservedStatus               string          `json:"observed_status,omitempty"`
	ObservedAt                   string          `json:"observed_at,omitempty"`
	ReferenceDigest              string          `json:"reference_digest,omitempty"`
	SummaryDigest                string          `json:"summary_digest,omitempty"`
	CorrectionOf                 string          `json:"correction_of,omitempty"`
}

func shopProofID(value string) bool {
	if len(value) == 0 || len(value) > 191 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func shopProofVersion(value string) bool {
	v, err := strconv.ParseInt(value, 10, 64)
	return err == nil && v > 0 && strconv.FormatInt(v, 10) == value
}

func isShopVerificationScope(scope string) bool {
	switch scope {
	case VerificationScopeShopInventoryReveal, VerificationScopeShopDeliveryCreate, VerificationScopeShopDeliveryReveal, VerificationScopeShopRiskRelease, VerificationScopeShopRefundManage, VerificationScopeShopAdjustmentManage, VerificationScopeShopEvidenceCreate:
		return true
	}
	return false
}

// gjson enumerates the original keys, preserving duplicate fields which a map
// decode would otherwise discard. JSON decoding remains in common.Unmarshal.
func shopProofFields(raw []byte, required, optional []string) bool {
	if common.GetJsonType(raw) != "object" {
		return false
	}
	seen := map[string]bool{}
	valid := true
	gjson.ParseBytes(raw).ForEach(func(key, value gjson.Result) bool {
		name := key.String()
		if seen[name] || (!slices.Contains(required, name) && !slices.Contains(optional, name)) || value.Type == gjson.Null {
			valid = false
			return false
		}
		seen[name] = true
		return true
	})
	for _, key := range required {
		if !seen[key] {
			return false
		}
	}
	return valid
}

func normalizeShopVerification(operation VerificationOperation) (ShopVerificationContext, error) {
	var result ShopVerificationContext
	invalid := func() (ShopVerificationContext, error) {
		return ShopVerificationContext{}, ErrVerificationContextInvalid
	}
	if len(operation.Context) > 16384 || common.Unmarshal(operation.Context, &result) != nil {
		return invalid()
	}
	reveal := operation.Scope == VerificationScopeShopInventoryReveal || operation.Scope == VerificationScopeShopDeliveryReveal
	required := []string{"action", "target_id", "expected_version", "details"}
	if !reveal {
		required = append(required, "idempotency_key")
	}
	if !shopProofFields(operation.Context, required, nil) || !shopProofID(result.TargetID) || !shopProofVersion(result.ExpectedVersion) {
		return invalid()
	}
	if !reveal {
		id, err := uuid.Parse(result.IdempotencyKey)
		if err != nil || id.String() != result.IdempotencyKey {
			return invalid()
		}
	}
	var fields map[string]common.RawMessage
	if common.Unmarshal(operation.Context, &fields) != nil {
		return invalid()
	}
	var needs, optional []string
	switch operation.Scope {
	case VerificationScopeShopInventoryReveal:
		if result.Action != "reveal" {
			return invalid()
		}
		needs = []string{"reason"}
	case VerificationScopeShopDeliveryReveal:
		if result.Action != "reveal" {
			return invalid()
		}
		needs = []string{"reason", "delivery_id"}
	case VerificationScopeShopDeliveryCreate:
		needs = []string{"reason", "template_version"}
		switch result.Action {
		case "first":
		case "replacement":
			needs = append(needs, "case_id", "case_version")
		default:
			return invalid()
		}
	case VerificationScopeShopRiskRelease:
		if result.Action != "release" {
			return invalid()
		}
		needs = []string{"reason", "evidence_ids"}
	case VerificationScopeShopRefundManage:
		switch result.Action {
		case "approve":
			needs = []string{"kind", "money", "reason", "evidence_ids"}
		case "register_attempt", "cancel":
			needs = []string{"reason", "evidence_ids"}
		case "record_attempt":
			needs = []string{"attempt_id", "operated_at", "evidence_ids"}
			optional = []string{"channel_ref", "public_note"}
		case "verify_attempt":
			needs = []string{"attempt_id", "conclusion", "evidence_ids"}
		default:
			return invalid()
		}
	case VerificationScopeShopAdjustmentManage:
		switch result.Action {
		case "record":
			needs = []string{"kind", "money", "provider_adjustment_ref", "state", "reason", "evidence_ids"}
		case "review":
			needs = []string{"adjustment_id", "state", "reason", "evidence_ids"}
		default:
			return invalid()
		}
	case VerificationScopeShopEvidenceCreate:
		if result.Action != "create" {
			return invalid()
		}
		needs = []string{"source", "provider_transaction_id", "money", "observed_status", "observed_at", "reference_digest", "summary_digest"}
		optional = []string{"provider_refund_or_adjustment_id", "correction_of"}
	default:
		return invalid()
	}
	if !shopProofFields(fields["details"], needs, optional) {
		return invalid()
	}
	// Presence alone is insufficient: an empty required scalar must not bypass
	// the per-value checks below and authorize a different financial operation.
	for _, key := range needs {
		if key == "money" || key == "evidence_ids" {
			continue
		}
		value := gjson.GetBytes(fields["details"], key)
		if value.Type != gjson.String || strings.TrimSpace(value.String()) == "" {
			return invalid()
		}
	}
	d := &result.Details
	for _, id := range []string{d.TemplateVersion, d.CaseID, d.DeliveryID, d.AttemptID, d.AdjustmentID, d.CorrectionOf} {
		if id != "" && !shopProofID(id) {
			return invalid()
		}
	}
	if d.CaseVersion != "" && !shopProofVersion(d.CaseVersion) || d.TemplateVersion != "" && !shopProofVersion(d.TemplateVersion) {
		return invalid()
	}
	d.Reason = strings.TrimSpace(d.Reason)
	if (slices.Contains(needs, "reason") && d.Reason == "") || utf8.RuneCountInString(d.Reason) > 2000 || utf8.RuneCountInString(d.PublicNote) > 2000 {
		return invalid()
	}
	if d.Money != nil {
		var details map[string]common.RawMessage
		amountValid := shopProofVersion(d.Money.AmountMinor) || operation.Scope == VerificationScopeShopEvidenceCreate && d.Money.AmountMinor == "0"
		if common.Unmarshal(fields["details"], &details) != nil || !shopProofFields(details["money"], []string{"amount_minor", "currency", "exponent"}, nil) || !amountValid || len(d.Money.Currency) != 3 || d.Money.Currency != strings.ToUpper(d.Money.Currency) || d.Money.Exponent < 0 || d.Money.Exponent > 3 {
			return invalid()
		}
		for _, c := range d.Money.Currency {
			if c < 'A' || c > 'Z' {
				return invalid()
			}
		}
	}
	if slices.Contains(needs, "evidence_ids") && len(d.EvidenceIDs) == 0 || len(d.EvidenceIDs) > 20 {
		return invalid()
	}
	for _, id := range d.EvidenceIDs {
		if !shopProofID(id) {
			return invalid()
		}
	}
	slices.Sort(d.EvidenceIDs)
	d.EvidenceIDs = slices.Compact(d.EvidenceIDs)
	for _, ref := range []string{d.ChannelRef, d.ProviderAdjustmentRef, d.ProviderTransactionID, d.ProviderRefundOrAdjustmentID} {
		if len(ref) > 191 || strings.ContainsAny(ref, "\r\n\x00") {
			return invalid()
		}
	}
	if d.Kind != "" {
		valid := slices.Contains([]string{"goods", "extra", "mismatch"}, d.Kind)
		if operation.Scope == VerificationScopeShopAdjustmentManage {
			valid = slices.Contains([]string{"external_refund", "chargeback"}, d.Kind)
		}
		if !valid {
			return invalid()
		}
	}
	if d.Conclusion != "" && !slices.Contains([]string{"succeeded", "failed", "unknown"}, d.Conclusion) {
		return invalid()
	}
	if d.State != "" && !slices.Contains([]string{"pending", "succeeded", "failed", "disputed"}, d.State) {
		return invalid()
	}
	if operation.Scope == VerificationScopeShopEvidenceCreate && (d.Source != "merchant_console" || d.ProviderTransactionID == "" || !slices.Contains([]string{"pending", "succeeded", "failed", "disputed", "unknown", "paid", "unpaid", "not_refunded", "refund_failed", "not_executed", "refund_succeeded"}, d.ObservedStatus)) {
		return invalid()
	}
	for _, value := range []string{d.OperatedAt, d.ObservedAt} {
		if value != "" {
			if parsed, err := time.Parse(time.RFC3339, value); err != nil || parsed.Unix() <= 0 || !strings.HasSuffix(value, "Z") {
				return invalid()
			}
		}
	}
	for _, value := range []string{d.ReferenceDigest, d.SummaryDigest} {
		if value != "" {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != 32 || strings.ToLower(value) != value {
				return invalid()
			}
		}
	}
	return result, nil
}
