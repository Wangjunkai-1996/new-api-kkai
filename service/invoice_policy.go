package service

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"net/mail"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

func (s InvoiceService) Policy() InvoicePolicy {
	p := InvoicePolicy{PolicyVersion: "unconfigured", InvoiceTypes: []InvoiceTypePolicy{}, Currencies: []InvoiceCurrencyPolicy{}, Sources: []string{}, Providers: []string{}, Timezone: "Asia/Shanghai", Limits: map[string]int{"max_orders": 50, "max_file_bytes": 20 * 1024 * 1024}}
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["InvoicePolicy"]
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		raw = os.Getenv("KKAI_INVOICE_POLICY")
	}
	if raw == "" || common.UnmarshalJsonStr(raw, &p) != nil {
		p.Enabled = false
		p.DisabledReason = "POLICY_UNCONFIGURED"
		return p
	}
	p.Limits = map[string]int{"max_orders": 50, "max_file_bytes": 20 * 1024 * 1024}
	valid := p.PolicyVersion != "" && len(p.PolicyVersion) <= 64 && p.SellerID != "" && len(p.SellerID) <= 64 && p.SellerName != "" && len(p.SellerName) <= 200 && p.TaxGroup != "" && len(p.TaxGroup) <= 64 && len(p.InvoiceTypes) > 0
	for _, it := range p.InvoiceTypes {
		if it.ID == "" || len(it.ID) > 64 || it.Label == "" {
			valid = false
		}
		for _, f := range it.RequiredFields {
			if !slices.Contains([]string{"tax_id", "address", "phone", "bank_name", "bank_account", "email"}, f) {
				valid = false
			}
		}
	}
	if len(p.Currencies) != 1 || p.Currencies[0].Currency != "CNY" || p.Currencies[0].Exponent != 2 {
		valid = false
	}
	// Only the main site's single Epay merchant has an audited receipt namespace.
	// Adding a provider in configuration must not bypass payment verification.
	if len(p.Providers) != 1 || p.Providers[0] != model.PaymentProviderEpay {
		valid = false
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		valid = false
	}
	for _, source := range p.Sources {
		if !slices.Contains([]string{"topup", "membership"}, source) {
			valid = false
		}
	}
	if !valid {
		p.Enabled = false
		p.DisabledReason = "POLICY_UNCONFIGURED"
	}
	if _, _, err := invoiceKeys(); err != nil {
		p.Enabled = false
		p.DisabledReason = "ENCRYPTION_UNAVAILABLE"
	}
	if p.Enabled {
		if _, err := InvoiceAssetStoreFromEnvironment(context.Background()); err != nil {
			p.Enabled = false
			p.DisabledReason = "STORAGE_UNAVAILABLE"
		}
	}
	if !p.Enabled && p.DisabledReason == "" {
		p.DisabledReason = "FEATURE_DISABLED"
	}
	return p
}
func invoiceValidateScope(scope InvoiceScope) error {
	if scope.TenantID != 0 || scope.UserID <= 0 {
		return invoiceError(403, "FORBIDDEN")
	}
	return nil
}
func invoiceValidateProfile(p *InvoiceProfileData, policy InvoicePolicy, invoiceType string) error {
	p.Kind = strings.TrimSpace(p.Kind)
	p.Title = strings.TrimSpace(p.Title)
	p.TaxID = strings.TrimSpace(p.TaxID)
	p.Email = strings.TrimSpace(p.Email)
	values := map[string]string{"title": p.Title, "tax_id": p.TaxID, "email": p.Email, "address": p.Address, "phone": p.Phone, "bank_name": p.BankName, "bank_account": p.BankAccount}
	limits := map[string]int{"title": 200, "tax_id": 128, "email": 254, "address": 500, "phone": 64, "bank_name": 200, "bank_account": 128}
	for field, value := range values {
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limits[field] || strings.ContainsAny(value, "\x00\r") {
			return &InvoiceError{Status: 400, Code: "VALIDATION_FAILED", FieldErrors: map[string]string{"profile." + field: "INVALID_VALUE"}}
		}
	}
	if !slices.Contains([]string{"person", "company"}, p.Kind) || p.Title == "" {
		return invoiceError(400, "VALIDATION_FAILED")
	}
	if p.Kind == "person" {
		p.TaxID = ""
	}
	if p.Kind == "company" && p.TaxID == "" {
		return &InvoiceError{Status: 400, Code: "VALIDATION_FAILED", FieldErrors: map[string]string{"profile.tax_id": "REQUIRED"}}
	}
	email, err := mail.ParseAddress(p.Email)
	if err != nil || email.Address != p.Email || strings.ContainsAny(p.Email, "\r\n,;") {
		return &InvoiceError{Status: 400, Code: "VALIDATION_FAILED", FieldErrors: map[string]string{"profile.email": "INVALID_EMAIL"}}
	}
	if invoiceType != "" {
		found := false
		for _, it := range policy.InvoiceTypes {
			if it.ID != invoiceType {
				continue
			}
			found = true
			for _, field := range it.RequiredFields {
				if strings.TrimSpace(values[field]) == "" {
					return &InvoiceError{Status: 400, Code: "VALIDATION_FAILED", FieldErrors: map[string]string{"profile." + field: "REQUIRED"}}
				}
			}
		}
		if !found {
			return invoiceError(422, "INVOICE_TYPE_UNSUPPORTED")
		}
	}
	return nil
}
