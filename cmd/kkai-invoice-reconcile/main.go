package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type manifest struct {
	Operator       string       `json:"operator"`
	EvidenceSource string       `json:"evidence_source"`
	EvidenceSHA256 string       `json:"evidence_sha256"`
	Orders         []orderProof `json:"orders"`
}

type orderProof struct {
	TenantID          int64  `json:"tenant_id"`
	UserID            int    `json:"user_id"`
	OrderNo           string `json:"order_no"`
	AmountMinor       int64  `json:"amount_minor"`
	Currency          string `json:"currency"`
	PaidAt            int64  `json:"paid_at"`
	ProviderReceiptID string `json:"provider_receipt_id"`
}

type orderResult struct {
	UserID          int    `json:"user_id"`
	OrderNo         string `json:"order_no"`
	AmountMinor     int64  `json:"amount_minor"`
	Currency        string `json:"currency"`
	PaidAt          int64  `json:"paid_at"`
	EstimatedPaidAt bool   `json:"estimated_paid_at,omitempty"`
	AmountRounded   bool   `json:"amount_rounded,omitempty"`
	Status          string `json:"status"`
}

type report struct {
	Mode           string        `json:"mode"`
	EvidenceSHA256 string        `json:"evidence_sha256"`
	Orders         []orderResult `json:"orders"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("kkai-invoice-reconcile", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "reviewed payment manifest JSON")
	evidencePath := flags.String("evidence", "", "original provider evidence file")
	apply := flags.Bool("apply", false, "record verified historical payment evidence")
	approveLegacy := flags.Bool("approve-legacy", false, "explicitly approve owner-recognized legacy payments without provider transaction IDs")
	ownerUserID := flags.Int("owner-user-id", 0, "optional customer restriction for legacy approval; zero allows explicit multi-user manifests")
	check := flags.Bool("check-selectable", false, "read-only verification of invoice selection and quote")
	dsn := flags.String("dsn", kkaischemacli.FirstNonEmpty(os.Getenv("KKAI_INVOICE_RECONCILE_DSN"), os.Getenv("SQL_DSN")), "database DSN; prefer --dsn-stdin")
	dsnStdin := flags.Bool("dsn-stdin", false, "read one database DSN from stdin")
	timeout := flags.Duration("timeout", time.Minute, "overall operation timeout")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *manifestPath == "" || *evidencePath == "" || *timeout <= 0 || (*apply && *check) {
		return errors.New("require --manifest FILE --evidence FILE and a DSN; default is preview, --apply enables writes")
	}
	input, err := loadManifest(*manifestPath, *evidencePath)
	if err != nil {
		return err
	}
	resolvedDSN, err := kkaischemacli.ResolveDSN(*dsn, *dsnStdin, stdin, "a database DSN or --dsn-stdin is required")
	if err != nil {
		return errors.New("database DSN input is missing or ambiguous")
	}
	// Driver errors and SQL logging can contain connection details or receipt IDs.
	previousLogger := logger.Default
	logger.Default = logger.Default.LogMode(logger.Silent)
	defer func() { logger.Default = previousLogger }()
	db, err := kkaischemacli.OpenDatabase(resolvedDSN)
	if err != nil {
		return errors.New("database connection failed")
	}
	raw, err := db.DB()
	if err != nil {
		return errors.New("database connection unavailable")
	}
	defer raw.Close()
	previousDialect := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseType(db.Dialector.Name()))
	defer common.SetMainDatabaseType(previousDialect)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *check {
		return checkSelectable(ctx, db, input, stdout)
	}
	var result *report
	var reconcileErr error
	if input.EvidenceSource == "owner_approved_legacy" {
		result, reconcileErr = reconcileLegacy(ctx, db, input, *ownerUserID, *apply, *approveLegacy)
	} else {
		if *approveLegacy {
			return errors.New("--approve-legacy requires owner_approved_legacy evidence")
		}
		result, reconcileErr = reconcile(ctx, db, input, *apply)
	}
	if result != nil {
		encoded, err := common.Marshal(result)
		if err != nil {
			return errors.New("could not encode reconciliation result")
		}
		if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
			return errors.New("could not write reconciliation result; inspect database before retrying")
		}
	}
	return reconcileErr
}

func loadManifest(manifestPath, evidencePath string) (manifest, error) {
	var input manifest
	file, err := os.Open(manifestPath)
	if err != nil {
		return input, errors.New("cannot read manifest")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || common.Unmarshal(raw, &input) != nil {
		return input, errors.New("manifest must be one JSON document of at most 1 MiB")
	}
	if !validText(input.Operator, 128) || (input.EvidenceSource != "signed_callback" && input.EvidenceSource != "merchant_statement" && input.EvidenceSource != "owner_approved_legacy") {
		return input, errors.New("manifest requires an operator and signed_callback, merchant_statement, or owner_approved_legacy evidence")
	}
	digest, err := hex.DecodeString(input.EvidenceSHA256)
	if err != nil || len(digest) != sha256.Size || input.EvidenceSHA256 != strings.ToLower(input.EvidenceSHA256) {
		return input, errors.New("manifest requires a lowercase SHA256 evidence digest")
	}
	evidence, err := os.Open(evidencePath)
	if err != nil {
		return input, errors.New("cannot read original evidence file")
	}
	defer evidence.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(evidence, (32<<20)+1))
	if err != nil || n == 0 || n > 32<<20 || hex.EncodeToString(hash.Sum(nil)) != input.EvidenceSHA256 {
		return input, errors.New("original evidence must be nonempty, at most 32 MiB, and match evidence_sha256")
	}
	return input, nil
}

func validText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

func reconcile(ctx context.Context, db *gorm.DB, input manifest, apply bool) (*report, error) {
	if len(input.Orders) == 0 || len(input.Orders) > 100 {
		return nil, errors.New("manifest requires between 1 and 100 orders")
	}
	result := &report{Mode: "preview", EvidenceSHA256: input.EvidenceSHA256, Orders: []orderResult{}}
	ids := make([]int, len(input.Orders))
	seenOrders, seenReceipts := map[string]bool{}, map[string]bool{}
	// Validate the complete manifest before the first write. Never use Money or
	// credited quota as payment evidence, even for successful legacy orders.
	for i, proof := range input.Orders {
		if proof.TenantID != model.PlatformTenantID || proof.UserID <= 0 || !validText(proof.OrderNo, 255) || !validText(proof.ProviderReceiptID, 255) || proof.AmountMinor <= 0 || proof.Currency != "CNY" || proof.PaidAt <= 0 || proof.PaidAt > common.GetTimestamp() || seenOrders[proof.OrderNo] || seenReceipts[proof.ProviderReceiptID] {
			return nil, errors.New("manifest contains invalid or duplicate payment evidence")
		}
		seenOrders[proof.OrderNo], seenReceipts[proof.ProviderReceiptID] = true, true
		var order model.TopUp
		if err := db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND trade_no = ?", proof.TenantID, proof.UserID, proof.OrderNo).First(&order).Error; err != nil {
			return nil, errors.New("order lookup or ownership validation failed; no payments changed")
		}
		if order.Status != common.TopUpStatusSuccess || order.PaymentProvider != model.PaymentProviderEpay || order.PaidMinor < 0 || (order.Currency != "" && order.Currency != "CNY") || proof.PaidAt < order.CreateTime {
			return nil, errors.New("order payment state, currency, or payment time is invalid; no payments changed")
		}
		var memberCount, subscriptionCount int64
		if db.WithContext(ctx).Model(&model.MembershipOrder{}).Where("trade_no = ?", proof.OrderNo).Count(&memberCount).Error != nil || db.WithContext(ctx).Model(&model.SubscriptionOrder{}).Where("trade_no = ?", proof.OrderNo).Count(&subscriptionCount).Error != nil || memberCount != 0 || subscriptionCount != 0 {
			return nil, errors.New("order aliases could not be excluded; no payments changed")
		}
		key := sha256.Sum256([]byte(model.PaymentProviderEpay + ":" + proof.ProviderReceiptID))
		var receipt model.InvoicePaymentReceipt
		var receipts []model.InvoicePaymentReceipt
		if err := db.WithContext(ctx).Where("top_up_id = ? OR receipt_key = ?", order.Id, hex.EncodeToString(key[:])).Find(&receipts).Error; err != nil {
			return nil, errors.New("invoice payment evidence lookup failed; no payments changed")
		}
		otherOrderReceipt := false
		for _, existing := range receipts {
			if existing.TopUpID == int64(order.Id) {
				receipt = existing
			} else {
				otherOrderReceipt = true
			}
		}
		if otherOrderReceipt && receipt.ID == 0 {
			return nil, errors.New("payment receipt already belongs to another order; no payments changed")
		}
		if receipt.ID == 0 && order.PaidMinor > 0 && order.PaidMinor != proof.AmountMinor {
			return nil, errors.New("exact order amount differs from payment evidence; no payments changed")
		}
		status := "ready"
		if receipt.ID != 0 {
			status = "already_verified"
			if receipt.Status != "verified" || receipt.ReceiptKey != hex.EncodeToString(key[:]) || receipt.AmountMinor != proof.AmountMinor || receipt.Currency != proof.Currency || receipt.UserID != proof.UserID || receipt.TenantID != proof.TenantID || receipt.Provider != model.PaymentProviderEpay {
				status = "payment_exception"
			}
		}
		paidAt := proof.PaidAt
		if receipt.PaidAt > 0 {
			paidAt = receipt.PaidAt // Replays preserve the original evidence time.
		}
		ids[i] = order.Id
		result.Orders = append(result.Orders, orderResult{UserID: proof.UserID, OrderNo: proof.OrderNo, AmountMinor: proof.AmountMinor, Currency: proof.Currency, PaidAt: paidAt, Status: status})
	}
	if err := db.WithContext(ctx).Select("id, actor_id, source, evidence_ref, evidence_hash, receipt_id, top_up_id, kind, status, reason_code, created_at").Limit(1).Find(&model.WalletPaymentEvent{}).Error; err != nil {
		return nil, errors.New("wallet payment audit schema unavailable; no payments changed")
	}
	if !apply {
		return result, nil
	}
	result.Mode = "apply"
	for i := range result.Orders {
		result.Orders[i].Status = "not_applied"
	}
	for i, proof := range input.Orders {
		// This call owns its transaction. In particular an exception has already
		// committed its finance hold when it returns ErrInvoicePaymentException.
		err := model.ReconcileInvoiceTopUpPayment(ctx, db, proof.TenantID, proof.UserID, proof.OrderNo, model.TopUpPaymentReceipt{ProviderReceiptID: proof.ProviderReceiptID, AmountMinor: proof.AmountMinor, Currency: proof.Currency}, proof.PaidAt)
		result.Orders[i].Status = "verified"
		if err != nil {
			result.Orders[i].Status = "failed"
			if !errors.Is(err, model.ErrInvoicePaymentException) {
				return result, errors.New("payment reconciliation failed; inspect per-order results before retrying")
			}
			result.Orders[i].Status = "payment_exception"
		}
		if auditErr := recordAudit(ctx, db, input, ids[i], result.Orders[i].Status); auditErr != nil {
			return result, errors.New("payment result committed but audit recording failed; retry the same manifest to repair audit")
		}
		if err != nil {
			return result, errors.New("payment evidence conflicts; financial hold preserved, remaining orders were not applied")
		}
	}
	return result, nil
}

func recordAudit(ctx context.Context, db *gorm.DB, input manifest, topUpID int, status string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.TopUp
		if err := model.InvoiceLockForUpdate(tx).Where("id = ?", topUpID).First(&order).Error; err != nil {
			return err
		}
		var existing model.WalletPaymentEvent
		query := tx.Where("top_up_id = ? AND kind = ? AND source = ? AND evidence_hash = ? AND status = ?", topUpID, "invoice_reconcile", input.EvidenceSource, input.EvidenceSHA256, status)
		if err := query.Limit(1).Find(&existing).Error; err != nil || existing.ID != 0 {
			return err
		}
		return tx.Create(&model.WalletPaymentEvent{TopUpID: topUpID, Source: input.EvidenceSource, EvidenceRef: "sha256:" + input.EvidenceSHA256[:16] + ";operator:" + input.Operator, EvidenceHash: input.EvidenceSHA256, Kind: "invoice_reconcile", Status: status, CreatedAt: common.GetTimestamp()}).Error
	})
}
