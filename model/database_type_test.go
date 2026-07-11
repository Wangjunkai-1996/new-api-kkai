package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

func TestSetDatabaseTypesRefreshesDerivedColumnReferences(t *testing.T) {
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	t.Cleanup(func() {
		SetDatabaseTypes(originalMainType, originalLogType)
	})

	SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	assert.Equal(t, common.DatabaseTypePostgreSQL, common.MainDatabaseType())
	assert.Equal(t, common.DatabaseTypePostgreSQL, common.LogDatabaseType())
	assert.Equal(t, `"group"`, commonGroupCol)
	assert.Equal(t, `"key"`, commonKeyCol)
	assert.Equal(t, "true", commonTrueVal)
	assert.Equal(t, "false", commonFalseVal)
	assert.Equal(t, `"group"`, logGroupCol)
	assert.Equal(t, `"key"`, logKeyCol)

	SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	assert.Equal(t, "`group`", commonGroupCol)
	assert.Equal(t, "`key`", commonKeyCol)
	assert.Equal(t, "1", commonTrueVal)
	assert.Equal(t, "0", commonFalseVal)
	assert.Equal(t, "`group`", logGroupCol)
	assert.Equal(t, "`key`", logKeyCol)
}
