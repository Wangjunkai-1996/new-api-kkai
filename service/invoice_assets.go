package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"gorm.io/gorm"
)

const (
	InvoiceAttachmentMaxBytes int64 = 20 << 20
	InvoiceMultipartMaxBytes  int64 = 21 << 20
	invoiceImageMaxPixels           = 20_000_000
)

// InvoiceAssetStore only supports server-side operations. Invoice downloads
// always pass through the session and ownership checks, never public URLs.
type InvoiceAssetStore interface {
	Get(context.Context, string) (VideoAssetObject, error)
	Put(context.Context, string, string, io.Reader, int64) error
	Delete(context.Context, []string) error
}

type InvoiceAssetService struct {
	DB    *gorm.DB
	Store InvoiceAssetStore
}

// Local storage uses the shared private application volume. S3 is optional and
// must use dedicated credentials and a private bucket, never the video bucket.
func InvoiceAssetStoreFromEnvironment(ctx context.Context) (InvoiceAssetStore, error) {
	if directory := os.Getenv("KKAI_INVOICE_STORAGE_DIR"); directory != "" {
		store := &localInvoiceAssetStore{directory: directory}
		root, err := store.openRoot()
		if err != nil {
			return nil, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
		}
		_ = root.Close()
		return store, nil
	}
	if os.Getenv("KKAI_INVOICE_S3_PRIVATE") != "true" {
		return nil, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	accessKey, err := invoiceSecret("KKAI_INVOICE_S3_ACCESS_KEY_ID")
	if err != nil {
		return nil, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	secretKey, err := invoiceSecret("KKAI_INVOICE_S3_SECRET_ACCESS_KEY")
	if err != nil {
		return nil, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	config := video_studio_setting.R2Config{
		Endpoint: os.Getenv("KKAI_INVOICE_S3_ENDPOINT"), Region: os.Getenv("KKAI_INVOICE_S3_REGION"),
		Bucket: os.Getenv("KKAI_INVOICE_S3_BUCKET"), AccessKeyID: accessKey,
		SecretAccessKey: secretKey,
	}
	if config.Region == "" {
		config.Region = "auto"
	}
	store, err := NewR2VideoAssetStore(ctx, config)
	if err != nil {
		return nil, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	return store, nil
}

func NewInvoiceAssetServiceFromEnvironment(ctx context.Context, db *gorm.DB) (*InvoiceAssetService, error) {
	store, err := InvoiceAssetStoreFromEnvironment(ctx)
	if err != nil {
		return nil, err
	}
	return &InvoiceAssetService{DB: db, Store: store}, nil
}

func (s InvoiceAssetService) Upload(ctx context.Context, scope InvoiceScope, publicID, key, purpose, fileName string, reader io.Reader) (*InvoiceAttachmentDTO, error) {
	if s.DB == nil || s.Store == nil || reader == nil {
		return nil, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	if scope.TenantID != 0 || scope.UserID <= 0 || key == "" || len(key) > 128 {
		return nil, invoiceError(http.StatusBadRequest, "VALIDATION_FAILED")
	}
	request, err := s.attachmentRequest(ctx, scope, publicID)
	if err != nil {
		return nil, err
	}
	if (scope.Admin && purpose != "invoice") || (!scope.Admin && purpose != "dispute_evidence") {
		return nil, invoiceError(http.StatusBadRequest, "VALIDATION_FAILED")
	}
	content, err := io.ReadAll(io.LimitReader(reader, InvoiceAttachmentMaxBytes+1))
	if err != nil {
		return nil, invoiceError(http.StatusBadRequest, "FILE_READ_FAILED")
	}
	if int64(len(content)) > InvoiceAttachmentMaxBytes {
		return nil, invoiceError(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE")
	}
	mimeType, err := validateInvoiceAttachment(content)
	if err != nil {
		return nil, err
	}
	fileName = invoiceFileName(fileName, mimeType)
	digest := sha256.Sum256(content)
	uploadDigest := sha256.Sum256([]byte(key))
	attachment := model.InvoiceAttachment{TenantID: scope.TenantID, RequestID: request.ID, UploaderID: scope.UserID, Purpose: purpose,
		UploadKey: hex.EncodeToString(uploadDigest[:]), SHA256: hex.EncodeToString(digest[:]), FileName: fileName,
		MIMEType: mimeType, Size: int64(len(content)), Status: "validated", CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	lookup := func(db *gorm.DB, out *model.InvoiceAttachment) error {
		return db.Where("tenant_id = ? AND request_id = ? AND uploader_id = ? AND purpose = ? AND upload_key = ?", scope.TenantID, request.ID, scope.UserID, purpose, attachment.UploadKey).First(out).Error
	}
	var existing model.InvoiceAttachment
	if err := lookup(s.DB.WithContext(ctx), &existing); err == nil {
		if existing.Status != "failed" || !invoiceUploadContentMatches(existing, attachment) {
			return invoiceUploadReplay(existing, attachment)
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !invoiceUploadAllowed(scope, request.Status, purpose) {
		return nil, invoiceError(http.StatusConflict, "INVALID_STATE")
	}
	if existing.ID == 0 {
		var token [24]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		attachment.PublicID = "ifa_" + hex.EncodeToString(token[:])
		attachment.ObjectKey = "invoices/private/" + strconv.FormatInt(scope.TenantID, 10) + "/" + strconv.FormatInt(request.ID, 10) + "/" + hex.EncodeToString(token[:])
	}
	attachment.Status = "uploading"
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.InvoiceRequest
		if err := model.InvoiceLockForUpdate(tx).Where("id = ? AND tenant_id = ?", request.ID, scope.TenantID).First(&current).Error; err != nil {
			return err
		}
		if (!scope.Admin && current.UserID != scope.UserID) || !invoiceUploadAllowed(scope, current.Status, purpose) {
			return invoiceError(http.StatusConflict, "INVALID_STATE")
		}
		if existing.ID == 0 {
			if err := tx.Create(&attachment).Error; err != nil {
				return err
			}
		} else {
			// A known failed transfer can safely retry the same object and upload
			// identity. Keep in-flight/unknown outcomes unavailable to a second writer.
			var failed model.InvoiceAttachment
			if err := model.InvoiceLockForUpdate(tx).Where("id = ? AND status = ? AND bound_at = 0", existing.ID, "failed").First(&failed).Error; err != nil {
				return err
			}
			if !invoiceUploadContentMatches(failed, attachment) {
				return invoiceError(http.StatusConflict, "IDEMPOTENCY_CONFLICT")
			}
			failed.Status, failed.ExpiresAt = "uploading", attachment.ExpiresAt
			if err := tx.Model(&failed).Updates(map[string]any{"status": failed.Status, "expires_at": failed.ExpiresAt}).Error; err != nil {
				return err
			}
			attachment = failed
		}
		// Bytes have already passed content validation. Reserve the issuance
		// evidence before object transfer so a concurrent hold/reject cannot
		// release this payment while an actual invoice is being uploaded.
		if purpose == "invoice" && !current.IssuedEvidence {
			return tx.Model(&model.InvoiceRequest{}).Where("id = ? AND tenant_id = ?", current.ID, scope.TenantID).Update("issued_evidence", true).Error
		}
		return nil
	})
	if err != nil {
		if lookup(s.DB.WithContext(ctx), &existing) == nil {
			return invoiceUploadReplay(existing, attachment)
		}
		return nil, err
	}
	// The staging row precedes object creation, so crashes and failed DB writes
	// remain discoverable by the expiry worker instead of leaking private objects.
	if err := s.Store.Put(ctx, attachment.ObjectKey, mimeType, bytes.NewReader(content), int64(len(content))); err != nil {
		_ = s.DB.WithContext(context.WithoutCancel(ctx)).Model(&attachment).Updates(map[string]any{"status": "failed", "expires_at": time.Now().Unix()}).Error
		return nil, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.InvoiceRequest
		if err := model.InvoiceLockForUpdate(tx).Where("id = ? AND tenant_id = ?", request.ID, scope.TenantID).First(&current).Error; err != nil {
			return err
		}
		if (!scope.Admin && current.UserID != scope.UserID) || !invoiceUploadAllowed(scope, current.Status, purpose) {
			return invoiceError(http.StatusConflict, "INVALID_STATE")
		}
		update := tx.Model(&model.InvoiceAttachment{}).Where("id = ? AND status = ? AND bound_at = 0", attachment.ID, "uploading").Update("status", "validated")
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return invoiceError(http.StatusConflict, "INVALID_STATE")
		}
		// A validated official invoice upload is evidence of actual issuance.
		// Preserve the current status/version: upload is not a delivery action.
		if purpose == "invoice" && !current.IssuedEvidence {
			return tx.Model(&model.InvoiceRequest{}).Where("id = ? AND tenant_id = ?", current.ID, scope.TenantID).Update("issued_evidence", true).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	attachment.Status = "validated"

	dto := invoiceAttachmentDTO(attachment)
	return &dto, nil
}

func (s InvoiceAssetService) Open(ctx context.Context, scope InvoiceScope, publicID, fileID string) (*model.InvoiceAttachment, VideoAssetObject, error) {
	if s.DB == nil || s.Store == nil {
		return nil, VideoAssetObject{}, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	request, err := s.attachmentRequest(ctx, scope, publicID)
	if err != nil {
		return nil, VideoAssetObject{}, err
	}
	var attachment model.InvoiceAttachment
	if err := s.DB.WithContext(ctx).Where("tenant_id = ? AND request_id = ? AND public_id = ? AND status = ?", scope.TenantID, request.ID, fileID, "validated").First(&attachment).Error; err != nil {
		return nil, VideoAssetObject{}, err
	}
	allowed := scope.Admin && (attachment.BoundAt > 0 || attachment.ExpiresAt > time.Now().Unix())
	if !scope.Admin && attachment.BoundAt > 0 {
		var count int64
		if attachment.Purpose == "invoice" {
			err = s.DB.WithContext(ctx).Model(&model.InvoiceDelivery{}).Where("tenant_id = ? AND request_id = ? AND attachment_id = ?", scope.TenantID, request.ID, attachment.ID).Count(&count).Error
		} else if attachment.Purpose == "dispute_evidence" && attachment.UploaderID == scope.UserID {
			err = s.DB.WithContext(ctx).Model(&model.InvoiceEvent{}).Where("tenant_id = ? AND request_id = ? AND attachment_id = ?", scope.TenantID, request.ID, attachment.ID).Count(&count).Error
			if err == nil && count == 0 {
				err = s.DB.WithContext(ctx).Model(&model.InvoiceDispute{}).Where("tenant_id = ? AND request_id = ? AND evidence_attachment_id = ?", scope.TenantID, request.ID, attachment.ID).Count(&count).Error
			}
		}
		if err != nil {
			return nil, VideoAssetObject{}, err
		}
		allowed = count > 0
	}
	if !allowed {
		return nil, VideoAssetObject{}, gorm.ErrRecordNotFound
	}
	object, err := s.Store.Get(ctx, attachment.ObjectKey)
	if err != nil {
		return nil, VideoAssetObject{}, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	if object.Body == nil || object.ContentLength != attachment.Size {
		if object.Body != nil {
			_ = object.Body.Close()
		}
		return nil, VideoAssetObject{}, invoiceError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE")
	}
	return &attachment, object, nil
}

func (s InvoiceAssetService) attachmentRequest(ctx context.Context, scope InvoiceScope, publicID string) (*model.InvoiceRequest, error) {
	if scope.TenantID != 0 || scope.UserID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	query := s.DB.WithContext(ctx).Where("tenant_id = ? AND public_id = ?", scope.TenantID, publicID)
	if !scope.Admin {
		query = query.Where("user_id = ?", scope.UserID)
	}
	var request model.InvoiceRequest
	if err := query.First(&request).Error; err != nil {
		return nil, err
	}
	return &request, nil
}

func invoiceUploadAllowed(scope InvoiceScope, status, purpose string) bool {
	if scope.Admin && purpose == "invoice" {
		return status == model.InvoiceApprovedForIssue || status == model.InvoiceDisputed || status == model.InvoiceAwaitingReconfirmation
	}
	if !scope.Admin && purpose == "dispute_evidence" {
		return status == model.InvoiceDelivered || status == model.InvoiceConfirmed || status == model.InvoiceDisputed || status == model.InvoiceAwaitingReconfirmation
	}
	return false
}

func invoiceUploadContentMatches(existing, input model.InvoiceAttachment) bool {
	return existing.SHA256 == input.SHA256 && existing.Size == input.Size && existing.MIMEType == input.MIMEType && existing.FileName == input.FileName
}

func invoiceUploadReplay(existing, input model.InvoiceAttachment) (*InvoiceAttachmentDTO, error) {
	if !invoiceUploadContentMatches(existing, input) {
		return nil, invoiceError(http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	}
	if existing.Status == "uploading" {
		return nil, invoiceError(http.StatusServiceUnavailable, "RETRY_LATER")
	}
	if existing.Status != "validated" || (existing.BoundAt == 0 && existing.ExpiresAt <= time.Now().Unix()) {
		return nil, invoiceError(http.StatusConflict, "ATTACHMENT_EXPIRED")
	}
	dto := invoiceAttachmentDTO(existing)
	return &dto, nil
}

func invoiceFileName(name, mimeType string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "invoice"
	}
	for len(name) > 200 || !utf8.ValidString(name) {
		name = name[:len(name)-1]
	}
	extension := ".pdf"
	if mimeType == "image/png" {
		extension = ".png"
	} else if mimeType == "image/jpeg" {
		extension = ".jpg"
	}
	return strings.TrimSuffix(name, filepath.Ext(name)) + extension
}

var invoicePDFObjectOffset = regexp.MustCompile(`^[0-9]+\s+[0-9]+\s+obj\b`)

func validateInvoiceAttachment(content []byte) (string, error) {
	invalid := invoiceError(http.StatusUnsupportedMediaType, "FILE_TYPE_UNSUPPORTED")
	if len(content) == 0 {
		return "", invalid
	}
	mimeType := http.DetectContentType(content)
	if mimeType == "image/png" || mimeType == "image/jpeg" {
		config, _, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > invoiceImageMaxPixels {
			return "", invalid
		}
		if _, _, err := image.Decode(bytes.NewReader(content)); err != nil {
			return "", invalid
		}
		return mimeType, nil
	}
	if mimeType != "application/pdf" || !bytes.HasPrefix(content, []byte("%PDF-1.")) && !bytes.HasPrefix(content, []byte("%PDF-2.")) {
		return "", invalid
	}
	trimmed := bytes.TrimSpace(content)
	if !bytes.HasSuffix(trimmed, []byte("%%EOF")) || bytes.Count(content, []byte(" obj")) > 100000 {
		return "", invalid
	}
	for _, token := range []string{"/JavaScript", "/JS", "/Launch", "/EmbeddedFile", "/RichMedia", "/OpenAction", "/AA", "/Encrypt"} {
		if bytes.Contains(content, []byte(token)) {
			return "", invalid
		}
	}
	start := bytes.LastIndex(content, []byte("startxref"))
	if start < 0 {
		return "", invalid
	}
	fields := bytes.Fields(content[start+len("startxref"):])
	if len(fields) < 2 {
		return "", invalid
	}
	offset, err := strconv.ParseInt(string(fields[0]), 10, 64)
	if err != nil || offset < 0 || offset >= int64(start) {
		return "", invalid
	}
	target := content[offset:]
	if !bytes.HasPrefix(target, []byte("xref")) && !invoicePDFObjectOffset.Match(target) {
		return "", invalid
	}
	return mimeType, nil
}

// CleanupExpired only removes unbound staging objects, never delivered invoices
// or user evidence. It holds the same attachment lock used by delivery binding.
func (s InvoiceAssetService) CleanupExpired(ctx context.Context, limit int) (int, error) {
	if s.DB == nil || s.Store == nil || limit < 1 || limit > 100 {
		return 0, fmt.Errorf("invalid invoice cleanup configuration")
	}
	var attachments []model.InvoiceAttachment
	if err := s.DB.WithContext(ctx).Where("bound_at = 0 AND expires_at <= ?", time.Now().Unix()).Order("id ASC").Limit(limit).Find(&attachments).Error; err != nil {
		return 0, err
	}
	removed := 0
	for _, attachment := range attachments {
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current model.InvoiceAttachment
			if err := model.InvoiceLockForUpdate(tx).Where("id = ? AND bound_at = 0 AND expires_at <= ?", attachment.ID, time.Now().Unix()).First(&current).Error; err != nil {
				return err
			}
			if err := s.Store.Delete(ctx, []string{current.ObjectKey}); err != nil {
				return err
			}
			return tx.Delete(&current).Error
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
