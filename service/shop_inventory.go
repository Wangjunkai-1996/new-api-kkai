package service

import (
	"context"
	"crypto/hmac"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type ShopInventoryRow struct {
	Identity     string            `json:"identity"`
	SecretFields []ShopSecretField `json:"secret_fields"`
}
type ShopInventoryImportRequest struct {
	ProductID       string             `json:"product_id"`
	ProductVersion  int64              `json:"product_version"`
	TemplateVersion int64              `json:"template_version"`
	Rows            []ShopInventoryRow `json:"rows"`
}
type ShopImportIssue struct {
	Row  int    `json:"row"`
	Code string `json:"code"`
}
type ShopInventoryPreview struct {
	PreviewDigest  string            `json:"preview_digest"`
	ValidCount     int               `json:"valid_count"`
	DuplicateCount int               `json:"duplicate_count"`
	InvalidCount   int               `json:"invalid_count"`
	Issues         []ShopImportIssue `json:"issues"`
}
type shopPreparedInventory struct {
	Product      model.ShopProduct
	Payloads     []shopSecretPayload
	Fingerprints [][]ShopFingerprint
	Preview      ShopInventoryPreview
}

func prepareShopInventory(ctx context.Context, db *gorm.DB, c model.ShopCommand, in ShopInventoryImportRequest) (*shopPreparedInventory, error) {
	if c.ActorID <= 0 || !c.Admin || c.TenantID != model.PlatformTenantID || in.ProductVersion <= 0 || in.TemplateVersion <= 0 || len(in.Rows) == 0 || len(in.Rows) > 200 {
		return nil, model.ErrShopInvalid
	}
	var actor model.User
	db = db.WithContext(ctx)
	if err := db.Select("id", "tenant_id", "status", "role").First(&actor, c.ActorID).Error; err != nil {
		return nil, model.ErrShopNotFound
	}
	if actor.Status != common.UserStatusEnabled || actor.Role < common.RoleAdminUser || actor.TenantID != model.PlatformTenantID {
		return nil, model.ErrShopNotFound
	}
	raw, err := common.Marshal(in)
	if err != nil || len(raw) > 1<<20 {
		return nil, model.ErrShopInvalid
	}
	product, err := model.GetShopProduct(db, model.ShopScope{UserID: c.ActorID, TenantID: c.TenantID, Admin: true}, in.ProductID)
	if err != nil {
		return nil, err
	}
	if product.Version != in.ProductVersion {
		return nil, model.ErrShopVersion
	}
	var terms model.ShopProductTerms
	if common.UnmarshalJsonStr(product.TermsJSON, &terms) != nil || terms.TemplateVersion != in.TemplateVersion || product.DeliveryType != "automatic" || product.IdentityNormalizationVersion != 1 || product.InventoryIdentityDomain == "" {
		return nil, model.ErrShopInvalid
	}
	result := &shopPreparedInventory{Product: *product, Payloads: make([]shopSecretPayload, len(in.Rows)), Fingerprints: make([][]ShopFingerprint, len(in.Rows)), Preview: ShopInventoryPreview{Issues: []ShopImportIssue{}}}
	seen := map[string]bool{}
	codes := make([]string, len(in.Rows))
	versions := map[string][]string{}
	domain := ""
	for i, row := range in.Rows {
		// Normalization v1 is explicitly trim-only; identifiers remain case-sensitive.
		identity := strings.TrimSpace(row.Identity)
		if identity == "" || len(identity) > 4096 || !utf8.ValidString(identity) || strings.ContainsAny(identity, "\r\n\x00") {
			codes[i] = "INVALID_IDENTITY"
			continue
		}
		payload, e := shopDeliveryPayload(terms, row.SecretFields)
		if e != nil {
			codes[i] = "INVALID_FIELDS"
			continue
		}
		fingerprints, e := ShopIdentityFingerprints(product.InventoryIdentityDomain, identity)
		if e != nil {
			return nil, e
		}
		result.Payloads[i], result.Fingerprints[i] = payload, fingerprints
		active := fingerprints[0]
		if seen[active.Digest] {
			codes[i] = "DUPLICATE_IDENTITY"
			continue
		}
		seen[active.Digest] = true
		domain = active.DomainHash
		for _, fp := range fingerprints {
			versions[fp.KeyVersion] = append(versions[fp.KeyVersion], fp.Digest)
		}
	}
	existing := map[string]bool{}
	for version, digests := range versions {
		var rows []model.ShopStockUnit
		if err = db.Select("fingerprint_key_version", "identity_fingerprint").Where("identity_domain_hash = ? AND fingerprint_key_version = ? AND identity_fingerprint IN ?", domain, version, digests).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			existing[row.FingerprintKeyVersion+":"+row.IdentityFingerprint] = true
		}
	}
	for i := range in.Rows {
		if codes[i] == "" {
			for _, fp := range result.Fingerprints[i] {
				if existing[fp.KeyVersion+":"+fp.Digest] {
					codes[i] = "DUPLICATE_IDENTITY"
					break
				}
			}
		}
		switch codes[i] {
		case "":
			result.Preview.ValidCount++
		case "DUPLICATE_IDENTITY":
			result.Preview.DuplicateCount++
		default:
			result.Preview.InvalidCount++
		}
		if codes[i] != "" {
			result.Preview.Issues = append(result.Preview.Issues, ShopImportIssue{Row: i + 1, Code: codes[i]})
		}
	}
	digest, version, err := ShopRequestHash([]any{c.ActorID, in}, "inventory-preview", "")
	if err != nil {
		return nil, err
	}
	result.Preview.PreviewDigest = "v1." + version + "." + digest
	return result, nil
}

func PreviewShopInventory(ctx context.Context, db *gorm.DB, c model.ShopCommand, in ShopInventoryImportRequest) (*ShopInventoryPreview, error) {
	result, err := prepareShopInventory(ctx, db, c, in)
	if err != nil {
		return nil, err
	}
	return &result.Preview, nil
}

func CommitShopInventory(ctx context.Context, db *gorm.DB, c model.ShopCommand, in ShopInventoryImportRequest, previewDigest string) (*model.ShopImportResult, error) {
	// Match/re-authorize committed idempotency before revalidating now-consumed stock.
	key, err := model.GetShopIdempotency(db.WithContext(ctx), model.ShopScope{UserID: c.ActorID, TenantID: c.TenantID, Admin: c.Admin}, "inventory-import", in.ProductID, c.Key)
	if err != nil {
		return nil, err
	}
	if key != nil {
		return model.ImportShopInventory(db.WithContext(ctx), model.ShopImportInput{Command: c, ProductPublicID: in.ProductID})
	}
	parts := strings.Split(previewDigest, ".")
	if len(parts) != 3 || parts[0] != "v1" || len(parts[2]) != 64 {
		return nil, model.ErrShopInvalid
	}
	expected, _, err := ShopRequestHash([]any{c.ActorID, in}, "inventory-preview", parts[1])
	if err != nil {
		return nil, err
	}
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, model.ErrShopProductChanged
	}
	prepared, err := prepareShopInventory(ctx, db, c, in)
	if err != nil {
		return nil, err
	}
	if prepared.Preview.InvalidCount > 0 {
		return nil, model.ErrShopInvalid
	}
	if prepared.Preview.DuplicateCount > 0 {
		return nil, model.ErrShopStock
	}
	rows := make([]model.ShopStockImportRow, 0, len(in.Rows))
	for i, payload := range prepared.Payloads {
		id, e := model.NewShopPublicID("su_")
		if e != nil {
			return nil, e
		}
		secret, e := SealShopSecret(payload, shopObjectBinding("inventory", id, ""))
		if e != nil {
			return nil, e
		}
		fps := prepared.Fingerprints[i]
		row := model.ShopStockImportRow{PublicID: id, Secret: secret, DomainHash: fps[0].DomainHash, Fingerprint: fps[0].Digest, FingerprintKeyVersion: fps[0].KeyVersion, NormalizationVersion: 1, MaskedLabel: "••••••"}
		for _, fp := range fps[1:] {
			row.HistoricalFingerprints = append(row.HistoricalFingerprints, model.ShopFingerprint{KeyVersion: fp.KeyVersion, Value: fp.Digest})
		}
		rows = append(rows, row)
	}
	return model.ImportShopInventory(db.WithContext(ctx), model.ShopImportInput{Command: c, ProductPublicID: in.ProductID, ProductVersion: in.ProductVersion, TemplateVersion: in.TemplateVersion, Rows: rows})
}
