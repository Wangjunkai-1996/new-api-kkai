package kkaimigrate

import "gorm.io/gorm"

var outboxEventKeySchemaStatements = map[string][]string{
	DialectSQLite: {},
	DialectMySQL: {
		`ALTER TABLE kkai_outbox MODIFY COLUMN event_key VARCHAR(191) NOT NULL`,
	},
	DialectPostgres: {
		`ALTER TABLE kkai_outbox ALTER COLUMN event_key TYPE VARCHAR(191)`,
	},
}

func ensureMySQL57OutboxBootstrap(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS kkai_outbox (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
event_key VARCHAR(191) NOT NULL UNIQUE,
topic VARCHAR(128) NOT NULL,
aggregate_id VARCHAR(128) NOT NULL DEFAULT '',
payload TEXT NOT NULL,
status VARCHAR(16) NOT NULL,
attempts INT NOT NULL DEFAULT 0,
available_at BIGINT NOT NULL,
locked_at BIGINT NOT NULL DEFAULT 0,
locked_by VARCHAR(128) NOT NULL DEFAULT '',
last_error TEXT NOT NULL,
created_at BIGINT NOT NULL,
delivered_at BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error
}
