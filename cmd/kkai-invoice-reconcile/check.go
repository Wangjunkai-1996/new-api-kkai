package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
)

func checkSelectable(ctx context.Context, db *gorm.DB, input manifest, stdout io.Writer) error {
	if len(input.Orders) == 0 || len(input.Orders) > 100 {
		return errors.New("selectability check requires between 1 and 100 orders")
	}
	var option model.Option
	if err := db.WithContext(ctx).Where(&model.Option{Key: "InvoicePolicy"}).First(&option).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("invoice policy lookup failed")
	}
	common.OptionMapRWMutex.Lock()
	previous, existed := common.OptionMap["InvoicePolicy"]
	wasNil := common.OptionMap == nil
	if wasNil {
		common.OptionMap = make(map[string]string)
	}
	// Policy uses the environment when the database has no nonempty override.
	common.OptionMap["InvoicePolicy"] = option.Value
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if wasNil {
			common.OptionMap = nil
		} else if existed {
			common.OptionMap["InvoicePolicy"] = previous
		} else {
			delete(common.OptionMap, "InvoicePolicy")
		}
	}()
	s := service.InvoiceService{DB: db}
	policy := s.Policy()
	if !policy.Enabled || len(policy.InvoiceTypes) == 0 {
		return fmt.Errorf("invoice policy unavailable: %s", policy.DisabledReason)
	}
	type selectableOrder struct {
		UserID         int      `json:"user_id"`
		OrderNo        string   `json:"order_no"`
		AmountMinor    int64    `json:"amount_minor"`
		Eligible       bool     `json:"eligible"`
		AllowedActions []string `json:"allowed_actions"`
	}
	type selectableTotal struct {
		UserID     int   `json:"user_id"`
		TotalMinor int64 `json:"total_minor"`
		Eligible   bool  `json:"eligible"`
	}
	result := struct {
		Orders     []selectableOrder `json:"orders"`
		Totals     []selectableTotal `json:"totals"`
		TotalMinor int64             `json:"total_minor"`
	}{}
	refs := make(map[int][]service.InvoiceOrderRef)
	totals := make(map[int]int64)
	seen := make(map[string]bool)
	users := []int{}
	for _, proof := range input.Orders {
		if proof.TenantID != 0 || proof.UserID <= 0 || !validText(proof.OrderNo, 255) || proof.Currency != "CNY" || proof.AmountMinor <= 0 || seen[proof.OrderNo] || result.TotalMinor > math.MaxInt64-proof.AmountMinor {
			return errors.New("invalid or duplicate order in selectability manifest")
		}
		seen[proof.OrderNo] = true
		scope := service.InvoiceScope{UserID: proof.UserID}
		// Exact filtering keeps the check valid when this user has over 100 other payments.
		page, err := s.Candidates(ctx, scope, service.InvoiceFilter{SourceType: "topup", OrderID: proof.OrderNo, PageSize: 100})
		if err != nil {
			return fmt.Errorf("candidate lookup failed for user %d order %s", proof.UserID, proof.OrderNo)
		}
		if len(page.Items) != 1 || page.Items[0].SourceType != "topup" || page.Items[0].OrderID != proof.OrderNo {
			return fmt.Errorf("order %s for user %d is not selectable", proof.OrderNo, proof.UserID)
		}
		candidate := page.Items[0]
		if !candidate.Eligible || !slices.Contains(candidate.AllowedActions, "select") || candidate.Currency != "CNY" || candidate.AmountMinor == nil || *candidate.AmountMinor != strconv.FormatInt(proof.AmountMinor, 10) {
			return fmt.Errorf("order %s for user %d has an ineligible or mismatched payment", proof.OrderNo, proof.UserID)
		}
		if _, exists := refs[proof.UserID]; !exists {
			users = append(users, proof.UserID)
		}
		refs[proof.UserID] = append(refs[proof.UserID], candidate.InvoiceOrderRef)
		totals[proof.UserID] += proof.AmountMinor
		result.TotalMinor += proof.AmountMinor
		result.Orders = append(result.Orders, selectableOrder{proof.UserID, proof.OrderNo, proof.AmountMinor, candidate.Eligible, candidate.AllowedActions})
	}
	for _, userID := range users {
		quote, err := s.Quote(ctx, service.InvoiceScope{UserID: userID}, service.InvoiceQuoteInput{Items: refs[userID], InvoiceType: policy.InvoiceTypes[0].ID, PolicyVersion: policy.PolicyVersion})
		if err != nil || !quote.Eligible || quote.Currency != "CNY" || quote.AmountMinor != strconv.FormatInt(totals[userID], 10) || len(quote.Items) != len(refs[userID]) {
			return fmt.Errorf("combined invoice quote failed for user %d", userID)
		}
		result.Totals = append(result.Totals, selectableTotal{userID, totals[userID], quote.Eligible})
	}
	encoded, err := common.Marshal(result)
	if err != nil {
		return errors.New("could not encode selectability result")
	}
	if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
		return errors.New("could not write selectability result")
	}
	return nil
}
