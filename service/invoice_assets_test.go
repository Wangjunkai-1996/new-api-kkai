package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func invoiceAssetFixture(t *testing.T) (InvoiceAssetService, []byte, InvoiceScope, InvoiceScope) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.InvoiceRequest{}, &model.InvoiceAttachment{}, &model.InvoiceDelivery{}, &model.InvoiceDispute{}, &model.InvoiceEvent{}))
	request := model.InvoiceRequest{PublicID: "owned-request", TenantID: 0, UserID: 7, Status: model.InvoiceApprovedForIssue, Version: 1}
	require.NoError(t, db.Create(&request).Error)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, img))
	return InvoiceAssetService{DB: db, Store: newMemoryVideoAssetStore()}, content.Bytes(), InvoiceScope{UserID: 7}, InvoiceScope{UserID: 1, Admin: true}
}

type failingInvoicePutStore struct {
	InvoiceAssetStore
	fail bool
}

func (s *failingInvoicePutStore) Put(ctx context.Context, key, contentType string, reader io.Reader, size int64) error {
	if s.fail {
		return fmt.Errorf("temporary object transfer failure")
	}
	return s.InvoiceAssetStore.Put(ctx, key, contentType, reader, size)
}

func TestInvoiceAssetsRetryKnownFailedUpload(t *testing.T) {
	s, content, _, admin := invoiceAssetFixture(t)
	ctx := context.Background()
	store := &failingInvoicePutStore{InvoiceAssetStore: s.Store, fail: true}
	s.Store = store
	_, err := s.Upload(ctx, admin, "owned-request", "retry-upload-key", "invoice", "invoice.png", bytes.NewReader(content))
	invoiceRequireCode(t, err, "STORAGE_UNAVAILABLE")
	var staged model.InvoiceAttachment
	require.NoError(t, s.DB.First(&staged).Error)
	_, err = s.Upload(ctx, admin, "owned-request", "retry-upload-key", "invoice", "other.png", bytes.NewReader(content))
	invoiceRequireCode(t, err, "IDEMPOTENCY_CONFLICT")
	store.fail = false
	attachment, err := s.Upload(ctx, admin, "owned-request", "retry-upload-key", "invoice", "invoice.png", bytes.NewReader(content))
	require.NoError(t, err, "same-file retry must recover after a known storage failure")
	assert.Equal(t, staged.PublicID, attachment.ID, "retry must preserve the upload identity")
	var count int64
	require.NoError(t, s.DB.Model(&model.InvoiceAttachment{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	meta, object, err := s.Open(ctx, admin, "owned-request", attachment.ID)
	require.NoError(t, err)
	defer object.Body.Close()
	got, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	assert.Equal(t, content, got)
	assert.Equal(t, staged.ObjectKey, meta.ObjectKey)
}

func TestInvoiceAssetsPrivateDeliveryAndUploadReplay(t *testing.T) {
	s, content, owner, admin := invoiceAssetFixture(t)
	ctx := context.Background()
	_, err := s.Upload(ctx, owner, "owned-request", "wrong-purpose", "invoice", "invoice.png", bytes.NewReader(content))
	require.Error(t, err)
	attachment, err := s.Upload(ctx, admin, "owned-request", "upload-key", "invoice", "../../发票.html", bytes.NewReader(content))
	require.NoError(t, err)
	assert.Equal(t, "发票.png", attachment.FileName)
	assert.Equal(t, "image/png", attachment.MIMEType)
	var request model.InvoiceRequest
	require.NoError(t, s.DB.Where("public_id = ?", "owned-request").First(&request).Error)
	assert.True(t, request.IssuedEvidence, "uploaded official invoice must prevent later rejection releasing its payment")
	assert.EqualValues(t, 1, request.Version, "upload must not invalidate the metadata form's request version")
	assert.Equal(t, model.InvoiceApprovedForIssue, request.Status, "upload is not delivery")
	replayed, err := s.Upload(ctx, admin, "owned-request", "upload-key", "invoice", "../../发票.html", bytes.NewReader(content))
	require.NoError(t, err)
	assert.Equal(t, attachment.ID, replayed.ID)
	_, _, err = s.Open(ctx, owner, "owned-request", attachment.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "upload is not delivery")
	_, _, err = s.Open(ctx, InvoiceScope{UserID: 8}, "owned-request", attachment.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var saved model.InvoiceAttachment
	require.NoError(t, s.DB.Where("public_id = ?", attachment.ID).First(&saved).Error)
	require.NoError(t, s.DB.Model(&saved).Update("bound_at", time.Now().Unix()).Error)
	require.NoError(t, s.DB.Create(&model.InvoiceDelivery{TenantID: 0, RequestID: saved.RequestID, AttachmentID: saved.ID, DeliveryVersion: 1}).Error)
	require.NoError(t, s.DB.Model(&model.InvoiceRequest{}).Where("id = ?", saved.RequestID).Update("status", model.InvoiceFinanceHold).Error)
	meta, object, err := s.Open(ctx, owner, "owned-request", attachment.ID)
	require.NoError(t, err, "finance hold must preserve delivered files")
	defer object.Body.Close()
	got, err := io.ReadAll(object.Body)
	require.NoError(t, err)
	assert.Equal(t, content, got)
	assert.Equal(t, attachment.ID, meta.PublicID)
	replayed, err = s.Upload(ctx, admin, "owned-request", "upload-key", "invoice", "../../发票.html", bytes.NewReader(content))
	require.NoError(t, err, "successful upload replay precedes current status eligibility")
	assert.Equal(t, attachment.ID, replayed.ID)
}

func TestInvoiceAssetsRejectInvalidFilesAndCrossRequestAttachment(t *testing.T) {
	s, content, _, admin := invoiceAssetFixture(t)
	ctx := context.Background()
	for _, content := range [][]byte{[]byte("<html>fake.pdf</html>"), []byte("%PDF-1.7\n%%EOF"), content[:len(content)/2]} {
		_, err := s.Upload(ctx, admin, "owned-request", "invalid", "invoice", "invoice.pdf", bytes.NewReader(content))
		var invoiceErr *InvoiceError
		require.ErrorAs(t, err, &invoiceErr)
		assert.Equal(t, "FILE_TYPE_UNSUPPORTED", invoiceErr.Code)
	}
	attachment, err := s.Upload(ctx, admin, "owned-request", "same-key", "invoice", "original.png", bytes.NewReader(content))
	require.NoError(t, err)
	_, err = s.Upload(ctx, admin, "owned-request", "same-key", "invoice", "other-name.png", bytes.NewReader(content))
	var invoiceErr *InvoiceError
	require.ErrorAs(t, err, &invoiceErr)
	assert.Equal(t, "IDEMPOTENCY_CONFLICT", invoiceErr.Code)
	require.NoError(t, s.DB.Create(&model.InvoiceRequest{PublicID: "another-request", UserID: 7, Status: model.InvoiceApprovedForIssue}).Error)
	_, _, err = s.Open(ctx, admin, "another-request", attachment.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestInvoiceAssetsCleanupNeverDeletesBoundEvidenceOrInvoice(t *testing.T) {
	s, content, _, admin := invoiceAssetFixture(t)
	ctx := context.Background()
	staged, err := s.Upload(ctx, admin, "owned-request", "staged-key", "invoice", "staged.png", bytes.NewReader(content))
	require.NoError(t, err)
	bound, err := s.Upload(ctx, admin, "owned-request", "bound-key", "invoice", "bound.png", bytes.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&model.InvoiceAttachment{}).Where("public_id = ?", bound.ID).Update("bound_at", time.Now().Unix()).Error)
	require.NoError(t, s.DB.Model(&model.InvoiceAttachment{}).Where("1=1").Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	count, err := s.CleanupExpired(ctx, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	var remaining []model.InvoiceAttachment
	require.NoError(t, s.DB.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	assert.Equal(t, bound.ID, remaining[0].PublicID)
	assert.NotEqual(t, staged.ID, remaining[0].PublicID)
}

func TestInvoicePDFStructureAndActiveContentValidation(t *testing.T) {
	prefix := "%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n"
	pdf := prefix + fmt.Sprintf("xref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer\n<< /Size 2 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", len(prefix))
	mimeType, err := validateInvoiceAttachment([]byte(pdf))
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", mimeType)
	_, err = validateInvoiceAttachment([]byte(strings.Replace(pdf, "/Type /Catalog", "/JavaScript", 1)))
	require.Error(t, err)
	_, err = validateInvoiceAttachment([]byte(strings.Replace(pdf, fmt.Sprintf("startxref\n%d", len(prefix)), "startxref\n99999999999999999", 1)))
	require.Error(t, err)
}
