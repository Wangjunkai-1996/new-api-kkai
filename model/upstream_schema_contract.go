package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type upstreamSchemaDefinition struct {
	Schema   int                     `json:"schema"`
	Dialects []upstreamDialectSchema `json:"dialects"`

	// Models is populated only by the dialect selector used during observation.
	Models []upstreamModelSchema `json:"-"`
}

type upstreamDialectSchema struct {
	Dialect string                `json:"dialect"`
	Models  []upstreamModelSchema `json:"models"`
}

type upstreamModelSchema struct {
	Table         string                      `json:"table"`
	Fields        []upstreamFieldSchema       `json:"fields"`
	UniqueIndexes []upstreamUniqueIndexSchema `json:"unique_indexes"`
}

type upstreamFieldSchema struct {
	Column        string                `json:"column"`
	TypeFamily    string                `json:"type_family"`
	TypeVariant   string                `json:"type_variant"`
	Length        int64                 `json:"length"`
	Precision     int64                 `json:"precision"`
	Scale         int64                 `json:"scale"`
	Unsigned      bool                  `json:"unsigned"`
	Nullable      bool                  `json:"nullable"`
	PrimaryKey    bool                  `json:"primary_key"`
	AutoIncrement bool                  `json:"auto_increment"`
	Default       upstreamDefaultSchema `json:"default"`
}

type upstreamUniqueIndexSchema struct {
	Columns   []string `json:"columns"`
	Predicate string   `json:"predicate"`
}

func upstreamPrimarySchemaModels() []any {
	return []any{
		&Channel{},
		&Token{},
		&User{},
		&PasskeyCredential{},
		&Option{},
		&Redemption{},
		&Ability{},
		&Log{},
		&Midjourney{},
		&TopUp{},
		&QuotaData{},
		&Task{},
		&Model{},
		&Vendor{},
		&PrefillGroup{},
		&Setup{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Checkin{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&CasbinRule{},
		&AuthzRole{},
	}
}

func upstreamOwnedSchemaModels() []any {
	models := upstreamPrimarySchemaModels()
	return append(models, &SubscriptionPlan{})
}

func CanonicalUpstreamSchema() ([]byte, error) {
	definition, err := canonicalUpstreamSchemaDefinition()
	if err != nil {
		return nil, err
	}
	return common.Marshal(definition)
}

func UpstreamSchemaDigest() (string, error) {
	encoded, err := CanonicalUpstreamSchema()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func canonicalUpstreamSchemaDefinition() (upstreamSchemaDefinition, error) {
	return canonicalUpstreamSchemaDefinitionForModels(upstreamOwnedSchemaModels())
}

func canonicalUpstreamSchemaDefinitionForModels(models []any) (upstreamSchemaDefinition, error) {
	definition := upstreamSchemaDefinition{Schema: 1}
	for _, dialector := range upstreamSchemaDialectors() {
		dialectDefinition, err := canonicalUpstreamDialectSchema(dialector, models)
		if err != nil {
			return upstreamSchemaDefinition{}, err
		}
		definition.Dialects = append(definition.Dialects, dialectDefinition)
	}
	return definition, nil
}

func canonicalUpstreamDialectSchema(dialector gorm.Dialector, models []any) (upstreamDialectSchema, error) {
	dialectDefinition := upstreamDialectSchema{Dialect: dialector.Name()}
	cache := &sync.Map{}
	naming := schema.NamingStrategy{}
	for _, candidate := range models {
		parsed, err := schema.Parse(candidate, cache, naming)
		if err != nil {
			return upstreamDialectSchema{}, fmt.Errorf("parse upstream schema model: %w", err)
		}
		modelDefinition := upstreamModelSchema{Table: parsed.Table}
		for _, field := range parsed.Fields {
			if field.DBName == "" || field.IgnoreMigration {
				continue
			}
			dataType := dialector.DataTypeOf(field)
			typeShape, err := normalizedColumnTypeShape(dialector.Name(), dataType, dataType)
			if err != nil {
				return upstreamDialectSchema{}, fmt.Errorf("model %s field %s: %w", parsed.Table, field.DBName, err)
			}
			defaultSchema, err := expectedDefaultSchema(field)
			if err != nil {
				return upstreamDialectSchema{}, fmt.Errorf("model %s field %s: %w", parsed.Table, field.DBName, err)
			}
			applyUpstreamFieldSemanticOverride(dialector.Name(), parsed.Table, field.DBName, &typeShape, &defaultSchema)
			modelDefinition.Fields = append(modelDefinition.Fields, upstreamFieldSchema{
				Column:        field.DBName,
				TypeFamily:    typeShape.Family,
				TypeVariant:   typeShape.Variant,
				Length:        typeShape.Length,
				Precision:     typeShape.Precision,
				Scale:         typeShape.Scale,
				Unsigned:      typeShape.Unsigned,
				Nullable:      !field.NotNull && !field.PrimaryKey,
				PrimaryKey:    field.PrimaryKey,
				AutoIncrement: field.AutoIncrement,
				Default:       defaultSchema,
			})
		}
		uniqueIndexes, err := expectedUniqueIndexes(parsed, dialector.Name())
		if err != nil {
			return upstreamDialectSchema{}, fmt.Errorf("model %s: %w", parsed.Table, err)
		}
		modelDefinition.UniqueIndexes = uniqueIndexes
		sort.Slice(modelDefinition.Fields, func(left, right int) bool {
			return modelDefinition.Fields[left].Column < modelDefinition.Fields[right].Column
		})
		dialectDefinition.Models = append(dialectDefinition.Models, modelDefinition)
	}
	sort.Slice(dialectDefinition.Models, func(left, right int) bool {
		return dialectDefinition.Models[left].Table < dialectDefinition.Models[right].Table
	})
	return dialectDefinition, nil
}

func upstreamSchemaDialectors() []gorm.Dialector {
	defaultMySQLDatetimePrecision := 3
	return []gorm.Dialector{
		sqlite.Dialector{},
		mysql.New(mysql.Config{
			SkipInitializeWithVersion: true,
			DefaultDatetimePrecision:  &defaultMySQLDatetimePrecision,
		}),
		postgres.New(postgres.Config{}),
	}
}

func upstreamSchemaDefinitionForDialect(definition upstreamSchemaDefinition, dialect string) (upstreamSchemaDefinition, error) {
	for _, candidate := range definition.Dialects {
		if candidate.Dialect == dialect {
			definition.Models = candidate.Models
			return definition, nil
		}
	}
	return upstreamSchemaDefinition{}, fmt.Errorf("upstream schema contract has no dialect %q", dialect)
}
