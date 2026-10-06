package model

import (
	"context"
	"gorm.io/gorm/logger"
	"strings"
	"sync"
	"time"
)

type migrationSQLRecorder struct {
	mu         sync.Mutex
	statements []string
}

func (recorder *migrationSQLRecorder) LogMode(logger.LogLevel) logger.Interface { return recorder }
func (recorder *migrationSQLRecorder) Info(context.Context, string, ...any)     {}
func (recorder *migrationSQLRecorder) Warn(context.Context, string, ...any)     {}
func (recorder *migrationSQLRecorder) Error(context.Context, string, ...any)    {}

func (recorder *migrationSQLRecorder) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	recorder.mu.Lock()
	recorder.statements = append(recorder.statements, statement)
	recorder.mu.Unlock()
}

func (recorder *migrationSQLRecorder) reset() {
	recorder.mu.Lock()
	recorder.statements = nil
	recorder.mu.Unlock()
}

func (recorder *migrationSQLRecorder) schemaMutations() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	mutations := make([]string, 0)
	for _, statement := range recorder.statements {
		normalized := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(normalized, "ALTER TABLE") ||
			strings.HasPrefix(normalized, "CREATE TABLE") ||
			strings.HasPrefix(normalized, "DROP TABLE") ||
			strings.HasPrefix(normalized, "RENAME TABLE") ||
			strings.HasPrefix(normalized, "CREATE INDEX") ||
			strings.HasPrefix(normalized, "CREATE UNIQUE INDEX") ||
			strings.HasPrefix(normalized, "DROP INDEX") {
			mutations = append(mutations, statement)
		}
	}
	return mutations
}
