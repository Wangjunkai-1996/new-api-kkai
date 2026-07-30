package kkaimigrate

var videoSampleCategorySchemaStatements = map[string][]migrationStatement{
	DialectSQLite: {
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE kkai_video_samples ADD COLUMN category VARCHAR(32)`},
	},
	DialectMySQL: {
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE kkai_video_samples ADD COLUMN category VARCHAR(32)`},
	},
	DialectPostgres: {
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE kkai_video_samples ADD COLUMN category VARCHAR(32)`},
	},
}
