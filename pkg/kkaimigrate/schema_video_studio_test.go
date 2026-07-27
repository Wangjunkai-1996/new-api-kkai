package kkaimigrate

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoStudioIdempotencyKeyIdentifierIsQuotedPerDialect(t *testing.T) {
	tests := map[string]string{
		DialectSQLite:   `"key"`,
		DialectMySQL:    "`key`",
		DialectPostgres: `"key"`,
	}
	for dialect, quotedKey := range tests {
		t.Run(dialect, func(t *testing.T) {
			var createTable string
			var createScopeIndex string
			for _, statement := range videoStudioSchemaStatements[dialect] {
				switch {
				case strings.HasPrefix(statement.SQL, "CREATE TABLE IF NOT EXISTS kkai_idempotency_keys"):
					createTable = statement.SQL
				case strings.HasPrefix(statement.SQL, "CREATE UNIQUE INDEX ux_kkai_idempotency_scope"):
					createScopeIndex = statement.SQL
				}
			}
			require.NotEmpty(t, createTable)
			require.NotEmpty(t, createScopeIndex)
			require.Contains(t, createTable, "\n"+quotedKey+" VARCHAR(128) NOT NULL")
			require.Contains(t, createScopeIndex, "(user_id, operation, "+quotedKey+")")
			require.NotContains(t, createTable, "\nkey VARCHAR(128) NOT NULL")
			require.NotContains(t, createScopeIndex, "(user_id, operation, key)")
		})
	}
}

func TestVideoStudioWorkerIndexesMatchOrderedScanQueries(t *testing.T) {
	expected := []string{
		"CREATE INDEX idx_kkai_video_generations_reconcile ON kkai_video_generations (deleted_at, id, task_id)",
		"CREATE INDEX idx_kkai_video_assets_upload_expiry ON kkai_video_assets (upload_expires_at, id, state)",
		"CREATE INDEX idx_kkai_idempotency_expiry ON kkai_idempotency_keys (expires_at, id)",
	}
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			statements := make(map[string]struct{}, len(videoStudioSchemaStatements[dialect]))
			for _, statement := range videoStudioSchemaStatements[dialect] {
				statements[statement.SQL] = struct{}{}
			}
			for _, sql := range expected {
				_, ok := statements[sql]
				require.True(t, ok, sql)
			}
		})
	}
}

func TestVideoStudioSQLiteWorkerPlansUseOrderedIndexes(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	_, err = ApplyOutboxEventKeyCompatibility(context.Background(), db, Options{})
	require.NoError(t, err)
	_, err = ApplyVideoStudioExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	_, err = ApplyVideoStudioExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE tasks (id INTEGER PRIMARY KEY, status VARCHAR(20) NOT NULL)").Error)

	type queryPlanRow struct {
		ID      int
		Parent  int
		NotUsed int
		Detail  string
	}
	tests := []struct {
		name      string
		query     string
		wantIndex string
	}{
		{
			name: "generation reconcile",
			query: `EXPLAIN QUERY PLAN
SELECT kkai_video_generations.id
FROM kkai_video_generations
JOIN tasks ON tasks.id = kkai_video_generations.task_id
WHERE kkai_video_generations.deleted_at = 0
  AND tasks.status = 'SUCCESS'
  AND NOT EXISTS (
    SELECT 1 FROM kkai_video_task_assets
    WHERE kkai_video_task_assets.task_id = kkai_video_generations.task_id
      AND kkai_video_task_assets.role = 'output'
  )
ORDER BY kkai_video_generations.id ASC
LIMIT 50`,
			wantIndex: "idx_kkai_video_generations_reconcile",
		},
		{
			name: "active upload expiry",
			query: `EXPLAIN QUERY PLAN
SELECT id
FROM kkai_video_assets
WHERE state IN ('pending_upload', 'deleting')
  AND upload_expires_at > 0
  AND upload_expires_at <= 2000000000
ORDER BY upload_expires_at ASC, id ASC
LIMIT 100`,
			wantIndex: "idx_kkai_video_assets_upload_expiry",
		},
		{
			name: "upload tombstone expiry",
			query: `EXPLAIN QUERY PLAN
SELECT id
FROM kkai_video_assets
WHERE state = 'deleted'
  AND upload_expires_at > 0
  AND upload_expires_at <= 2000000000
ORDER BY upload_expires_at ASC, id ASC
LIMIT 100`,
			wantIndex: "idx_kkai_video_assets_upload_expiry",
		},
		{
			name: "idempotency expiry",
			query: `EXPLAIN QUERY PLAN
SELECT id
FROM kkai_idempotency_keys
WHERE expires_at <= 2000000000
ORDER BY expires_at ASC, id ASC
LIMIT 500`,
			wantIndex: "idx_kkai_idempotency_expiry",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var rows []queryPlanRow
			require.NoError(t, db.Raw(test.query).Scan(&rows).Error)
			details := make([]string, 0, len(rows))
			for _, row := range rows {
				details = append(details, row.Detail)
			}
			plan := strings.Join(details, "\n")
			require.Contains(t, plan, test.wantIndex)
			require.NotContains(t, strings.ToUpper(plan), "USE TEMP B-TREE FOR ORDER BY")
		})
	}
}
