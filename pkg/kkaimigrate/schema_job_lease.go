package kkaimigrate

var jobLeaseSchemaStatements = map[string][]string{
	DialectSQLite: {
		`CREATE TABLE IF NOT EXISTS kkai_job_leases (
lease_name VARCHAR(128) PRIMARY KEY,
holder VARCHAR(128) NOT NULL,
lease_until BIGINT NOT NULL,
fence BIGINT NOT NULL,
updated_at BIGINT NOT NULL
)`,
	},
	DialectMySQL: {
		`CREATE TABLE IF NOT EXISTS kkai_job_leases (
lease_name VARCHAR(128) PRIMARY KEY,
holder VARCHAR(128) NOT NULL,
lease_until BIGINT NOT NULL,
fence BIGINT NOT NULL,
updated_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	},
	DialectPostgres: {
		`CREATE TABLE IF NOT EXISTS kkai_job_leases (
lease_name VARCHAR(128) PRIMARY KEY,
holder VARCHAR(128) NOT NULL,
lease_until BIGINT NOT NULL,
fence BIGINT NOT NULL,
updated_at BIGINT NOT NULL
)`,
	},
}

var jobLeaseIndexes = []indexSpec{
	{Name: "idx_kkai_job_leases_until", Table: "kkai_job_leases", Columns: []string{"lease_until"}},
}
