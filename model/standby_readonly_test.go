package model

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type standbyReadOnlyFixture struct {
	ID    int `gorm:"primaryKey"`
	Value string
}

func TestStandbyReadOnlyGuardRejectsORMAndRawWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standby-readonly?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&standbyReadOnlyFixture{}))
	require.NoError(t, EnableStandbyReadOnlyGuard(db))

	err = db.Create(&standbyReadOnlyFixture{ID: 1, Value: "create"}).Error
	require.True(t, errors.Is(err, ErrStandbyReadOnly))
	err = db.Model(&standbyReadOnlyFixture{}).Where("id = ?", 1).Update("value", "update").Error
	require.True(t, errors.Is(err, ErrStandbyReadOnly))
	err = db.Delete(&standbyReadOnlyFixture{}, 1).Error
	require.True(t, errors.Is(err, ErrStandbyReadOnly))
	err = db.Exec("INSERT INTO standby_read_only_fixtures (id, value) VALUES (1, 'raw')").Error
	require.True(t, errors.Is(err, ErrStandbyReadOnly))
}

func TestStandbyReadOnlyGuardAllowsReads(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standby-readonly-reads?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&standbyReadOnlyFixture{}))
	require.NoError(t, EnableStandbyReadOnlyGuard(db))

	var count int64
	require.NoError(t, db.Model(&standbyReadOnlyFixture{}).Count(&count).Error)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM standby_read_only_fixtures").Scan(&count).Error)
	require.Zero(t, count)
}

func TestStandbyRawSQLReadOnlyClassifierIsConservative(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		want      bool
	}{
		{name: "select", statement: "SELECT COUNT(*) FROM channels", want: true},
		{name: "show", statement: "SHOW CREATE TABLE channels", want: true},
		{name: "read pragma", statement: "PRAGMA table_info(`channels`)", want: true},
		{name: "write pragma", statement: "PRAGMA user_version = 3", want: false},
		{name: "row lock", statement: "SELECT * FROM channels FOR UPDATE", want: false},
		{name: "sequence", statement: "SELECT nextval('channels_id_seq')", want: false},
		{name: "stacked statement", statement: "SELECT 1; DELETE FROM channels", want: false},
		{name: "cte rejected without parser", statement: "WITH rows AS (SELECT 1) SELECT * FROM rows", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, standbyRawSQLIsReadOnly(test.statement))
		})
	}
}
