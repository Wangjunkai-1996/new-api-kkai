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
		},
		{
			Version:          5,
			Name:             "video_studio",
			Kind:             MigrationKindExpand,
			ImplementationID: "video_studio_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       videoStudioSchemaStatements,
		},
		{
			Version:          6,
			Name:             "video_sample_category",
			Kind:             MigrationKindExpand,
			ImplementationID: "video_sample_category_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       videoSampleCategorySchemaStatements,
		},
		{
			Version:          7,
			Name:             "image_studio",
			Kind:             MigrationKindExpand,
			ImplementationID: "image_studio_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       imageStudioSchemaStatements,
		},
		{
			Version:          8,
			Name:             "stateless_authentication",
			Kind:             MigrationKindExpand,
			ImplementationID: "stateless_authentication_v2",
			ChecksumVersion:  migrationChecksumSchemaBackfill,
			Statements:       authenticationSchemaStatements,
			BackfillSpec:     "after dialect-safe additive defaults commit, initialize users.auth_version and insert legacy Telegram claims with one set operation; reject ambiguous ownership by aggregate count",
			BackfillID:       "backfill_stateless_authentication_v3",
			Backfill:         backfillAuthenticationSchema,
		},
		rc41ArchitectureMigration(),
		{
			Version:          ImageAsyncSchemaVersion,
			Name:             "image_studio_async_uploads",
			Kind:             MigrationKindExpand,
			ImplementationID: "image_studio_async_uploads_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       imageAsyncSchemaStatements,
		},
		{
			Version:          MembershipSchemaVersion,
			Name:             "subscription_membership",
			Kind:             MigrationKindExpand,
			ImplementationID: "subscription_membership_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       membershipSchemaStatements,
		},
		{
			Version:          TenantSchemaVersion,
			Name:             "tenant_brand_reseller_contract",
			Kind:             MigrationKindContract,
			ImplementationID: "tenant_brand_reseller_gorm_v12",
			ChecksumVersion:  migrationChecksumSchemaGORM,
			Statements:       map[string][]migrationStatement{DialectSQLite: {}, DialectMySQL: {}, DialectPostgres: {}},
			SchemaSpec:       tenantSchemaSpec(),
			SchemaApply:      applyTenantSchema,
		},
		{
			Version:          TenantCompatibilitySchemaVersion,
			Name:             "tenant_brand_reseller_legacy_compatibility",
			Kind:             MigrationKindContract,
			ImplementationID: "tenant_brand_reseller_legacy_gorm_v13",
			ChecksumVersion:  migrationChecksumSchemaGORM,
			Statements:       map[string][]migrationStatement{DialectSQLite: {}, DialectMySQL: {}, DialectPostgres: {}},
			SchemaSpec:       tenantCompatibilitySchemaSpec(),
			SchemaApply:      applyTenantCompatibilitySchema,
		},
		{
			Version:          TenantOnlineSchemaVersion,
			Name:             "tenant_brand_reseller_online_contract",
			Kind:             MigrationKindContract,
			ImplementationID: "tenant_brand_reseller_online_v14",
			ChecksumVersion:  migrationChecksumSchemaGORM,
			Statements:       map[string][]migrationStatement{DialectSQLite: {}, DialectMySQL: {}, DialectPostgres: {}},
			SchemaSpec:       tenantOnlineSchemaSpec(),
			SchemaApply:      rejectTenantOnlineAutomaticApply,
		},
		{
			Version:          ResellerApplicationSchemaVersion,
			Name:             "reseller_application_domain_prefix",
			Kind:             MigrationKindExpand,
			ImplementationID: "reseller_application_domain_prefix_v15",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       resellerApplicationSchemaStatements,
		},
		{
			Version:          RequestTicketSchemaVersion,
			Name:             "request_tickets_and_notifications",
			Kind:             MigrationKindExpand,
			ImplementationID: "request_tickets_and_notifications_v16",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       requestTicketSchemaStatements,
		},
		{
			Version:          InvoiceSchemaVersion,
			Name:             "invoice_center",
			Kind:             MigrationKindExpand,
			ImplementationID: "invoice_center_v17",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       invoiceSchemaStatements,
		},
		{
			Version:          WalletPaymentSchemaVersion,
			Name:             "wallet_payment_lifecycle",
			Kind:             MigrationKindExpand,
			ImplementationID: "wallet_payment_lifecycle_v18",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       walletPaymentSchemaStatements,
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
