package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func invoiceDigest(input any) (string, error) {
	raw, err := common.Marshal(input)
	if err != nil {
		return "", err
	}
	return invoiceHash(string(raw)), nil
}
func invoiceOperation(db *gorm.DB, scope InvoiceScope, operation, key string, input any) (*model.InvoiceOperation, bool, error) {
	if len(key) < 8 || len(key) > 128 || strings.TrimSpace(key) != key {
		return nil, false, invoiceError(400, "IDEMPOTENCY_KEY_REQUIRED")
	}
	digest, err := invoiceDigest(input)
	if err != nil {
		return nil, false, err
	}
	row := model.InvoiceOperation{TenantID: scope.TenantID, ActorID: scope.UserID, Operation: invoiceHash(operation), OperationKey: invoiceHash(key), Digest: digest, CreatedAt: time.Now().Unix()}
	err = model.InvoiceLockForUpdate(db).Where("tenant_id = ? AND actor_id = ? AND operation = ? AND operation_key = ?", row.TenantID, row.ActorID, row.Operation, row.OperationKey).First(&row).Error
	if err == nil {
		if row.Digest != digest {
			return nil, false, invoiceError(409, "IDEMPOTENCY_CONFLICT")
		}
		return &row, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if err = db.Create(&row).Error; err != nil {
		return nil, false, invoiceError(409, "RETRY_LATER")
	}
	return &row, false, nil
}
func invoiceFinishOperation(tx *gorm.DB, operation *model.InvoiceOperation, id, code string) error {
	return tx.Model(operation).Updates(map[string]any{"result_id": id, "result_code": code}).Error
}
func invoiceMask(value string) string {
	r := []rune(value)
	if len(r) <= 4 {
		return "****"
	}
	return string(r[:1]) + "***" + string(r[len(r)-3:])
}
func invoiceProfileDTO(row model.InvoiceProfile, masked bool) (InvoiceProfileDTO, error) {
	result := InvoiceProfileDTO{ID: row.PublicID, Version: row.Version}
	if err := invoiceDecrypt(invoiceAAD(row.TenantID, row.UserID, row.PublicID, "profile"), row.DataCipher, &result.InvoiceProfileData); err != nil {
		return result, err
	}
	if masked {
		result.Title = invoiceMask(result.Title)
		if result.TaxID != "" {
			result.TaxID = invoiceMask(result.TaxID)
		}
		result.Email = invoiceMask(result.Email)
		result.Address = ""
		result.Phone = ""
		result.BankName = ""
		result.BankAccount = ""
	}
	return result, nil
}
func (s InvoiceService) Profiles(ctx context.Context, scope InvoiceScope, f InvoiceFilter) (InvoicePage[InvoiceProfileDTO], error) {
	result := InvoicePage[InvoiceProfileDTO]{Items: []InvoiceProfileDTO{}}
	if err := invoiceValidateScope(scope); err != nil {
		return result, err
	}
	if err := invoicePaging(&f); err != nil {
		return result, err
	}
	result.P, result.PageSize = f.P, f.PageSize
	q := s.database(ctx).Model(&model.InvoiceProfile{}).Where("tenant_id = ? AND user_id = ? AND deleted_at = 0", scope.TenantID, scope.UserID)
	if err := q.Count(&result.Total).Error; err != nil {
		return result, err
	}
	var rows []model.InvoiceProfile
	if err := q.Order("updated_at DESC, id DESC").Offset((f.P - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error; err != nil {
		return result, err
	}
	for _, row := range rows {
		dto, err := invoiceProfileDTO(row, true)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, dto)
	}
	return result, nil
}
func (s InvoiceService) GetProfile(ctx context.Context, scope InvoiceScope, id string) (InvoiceProfileDTO, error) {
	result := InvoiceProfileDTO{}
	if err := invoiceValidateScope(scope); err != nil {
		return result, err
	}
	var row model.InvoiceProfile
	if err := s.database(ctx).Where("tenant_id = ? AND user_id = ? AND public_id = ? AND deleted_at = 0", scope.TenantID, scope.UserID, id).First(&row).Error; err != nil {
		return result, err
	}
	return invoiceProfileDTO(row, false)
}
func (s InvoiceService) SaveProfile(ctx context.Context, scope InvoiceScope, id, key string, input InvoiceProfileInput) (InvoiceProfileDTO, error) {
	if err := invoiceValidateScope(scope); err != nil {
		return InvoiceProfileDTO{}, err
	}
	operationName := "profile:create"
	if id != "" {
		operationName = "profile:save:" + id
	}
	resultID := id
	err := s.database(ctx).Transaction(func(tx *gorm.DB) error {
		op, replay, err := invoiceOperation(tx, scope, operationName, key, input)
		if err != nil {
			return err
		}
		if replay {
			resultID = op.ResultID
			return nil
		}
		if err := invoiceValidateProfile(&input.InvoiceProfileData, s.Policy(), ""); err != nil {
			return err
		}
		now := time.Now().Unix()
		row := model.InvoiceProfile{}
		if id == "" {
			if resultID, err = invoiceID("ip_"); err != nil {
				return err
			}
			row = model.InvoiceProfile{PublicID: resultID, TenantID: scope.TenantID, UserID: scope.UserID, Version: 1, CreatedAt: now, UpdatedAt: now}
		} else {
			if err = model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND user_id = ? AND public_id = ? AND deleted_at = 0", scope.TenantID, scope.UserID, id).First(&row).Error; err != nil {
				return err
			}
			if row.Version != input.Version {
				return invoiceError(409, "VERSION_CONFLICT")
			}
			row.Version++
			row.UpdatedAt = now
		}
		cipher, err := invoiceEncrypt(invoiceAAD(scope.TenantID, scope.UserID, resultID, "profile"), input.InvoiceProfileData)
		if err != nil {
			return err
		}
		row.DataCipher = cipher
		if id == "" {
			err = tx.Create(&row).Error
		} else {
			update := tx.Model(&model.InvoiceProfile{}).Where("id = ? AND version = ?", row.ID, input.Version).Updates(map[string]any{"data_cipher": cipher, "version": row.Version, "updated_at": now})
			err = update.Error
			if err == nil && update.RowsAffected != 1 {
				return invoiceError(409, "VERSION_CONFLICT")
			}
		}
		if err != nil {
			return err
		}
		return invoiceFinishOperation(tx, op, resultID, "")
	})
	if err != nil {
		return InvoiceProfileDTO{}, err
	}
	return s.GetProfile(ctx, scope, resultID)
}
func (s InvoiceService) DeleteProfile(ctx context.Context, scope InvoiceScope, id, key string, version int64) error {
	if err := invoiceValidateScope(scope); err != nil {
		return err
	}
	return s.database(ctx).Transaction(func(tx *gorm.DB) error {
		op, replay, err := invoiceOperation(tx, scope, "profile:delete:"+id, key, version)
		if err != nil || replay {
			return err
		}
		var row model.InvoiceProfile
		if err = model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND user_id = ? AND public_id = ?", scope.TenantID, scope.UserID, id).First(&row).Error; err != nil {
			return err
		}
		if row.DeletedAt == 0 {
			if row.Version != version {
				return invoiceError(409, "VERSION_CONFLICT")
			}
			update := tx.Model(&model.InvoiceProfile{}).Where("id = ? AND version = ?", row.ID, version).Updates(map[string]any{"deleted_at": time.Now().Unix(), "version": version + 1})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return invoiceError(409, "VERSION_CONFLICT")
			}
		}
		return invoiceFinishOperation(tx, op, id, "")
	})
}
