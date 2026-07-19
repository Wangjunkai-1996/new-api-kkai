package kkaimigrate

func migrationSet() []migration {
	return []migration{
		{
			Version:          1,
			Name:             "risk_incidents_and_outbox",
			Kind:             MigrationKindExpand,
			ImplementationID: "risk_incidents_and_outbox_v1",
			ChecksumVersion:  migrationChecksumSchemaLegacy,
			Statements:       migrationStatements(migrationOperationCreateTable, riskSchemaStatements),
			Indexes:          riskIndexes,
			LegacyImportSpec: "copy policy_incident_events by legacy id; omit token names and raw content; never replay actions",
			LegacyImportID:   "import_legacy_policy_incidents_v1",
			ImportLegacy:     importLegacyPolicyIncidents,
		},
		{
			Version:          2,
			Name:             "internal_balance_ledger",
			Kind:             MigrationKindExpand,
			ImplementationID: "internal_balance_ledger_v1",
			ChecksumVersion:  migrationChecksumSchemaLegacy,
			Statements:       migrationStatements(migrationOperationCreateTable, ledgerSchemaStatements),
			Indexes:          ledgerIndexes,
			LegacyImportSpec: "copy internal_balance_adjustments by operation_id; preserve balances and reversal link; never reapply delta",
			LegacyImportID:   "import_legacy_balance_adjustments_v1",
			ImportLegacy:     importLegacyBalanceAdjustments,
		},
		{
			Version:          3,
			Name:             "background_job_leases",
			Kind:             MigrationKindExpand,
			ImplementationID: "background_job_leases_v1",
			ChecksumVersion:  migrationChecksumSchemaLegacy,
			Statements:       migrationStatements(migrationOperationCreateTable, jobLeaseSchemaStatements),
			Indexes:          jobLeaseIndexes,
		},
		{
			Version:          4,
			Name:             "outbox_event_key_mysql57_compat",
			Kind:             MigrationKindContract,
			ImplementationID: "outbox_event_key_mysql57_compat_v1",
			ChecksumVersion:  migrationChecksumSchemaLegacy,
			Statements:       migrationStatements(migrationOperationContract, outboxEventKeySchemaStatements),
			ApplyDialects:    []string{DialectMySQL},
			LegacyDialects:   []string{DialectSQLite, DialectPostgres},
		},
	}
}

func migrationStatements(operation string, statements map[string][]string) map[string][]migrationStatement {
	result := make(map[string][]migrationStatement, len(statements))
	for dialect, dialectStatements := range statements {
		result[dialect] = make([]migrationStatement, 0, len(dialectStatements))
		for _, statement := range dialectStatements {
			result[dialect] = append(result[dialect], migrationStatement{Operation: operation, SQL: statement})
		}
	}
	return result
}
