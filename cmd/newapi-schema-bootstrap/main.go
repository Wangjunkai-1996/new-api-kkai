package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/model"
)

var sourceRevision = "development"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("newapi-schema-bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dsn := flags.String("dsn", strings.TrimSpace(os.Getenv("SQL_DSN")), "database DSN")
	dsnFromStdin := flags.Bool("dsn-stdin", false, "read one database DSN from stdin")
	printSourceRevision := flags.Bool("source-revision", false, "print the compiled source revision and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *printSourceRevision {
		databaseOptionSet := false
		flags.Visit(func(flagValue *flag.Flag) {
			if flagValue.Name == "dsn" || flagValue.Name == "dsn-stdin" {
				databaseOptionSet = true
			}
		})
		if databaseOptionSet {
			return fmt.Errorf("--source-revision cannot be combined with database options")
		}
		_, err := fmt.Fprintln(stdout, sourceRevision)
		return err
	}

	resolvedDSN, err := kkaischemacli.ResolveDSN(
		*dsn,
		*dsnFromStdin,
		stdin,
		"SQL_DSN, --dsn, or --dsn-stdin is required",
	)
	if err != nil {
		return err
	}
	if err := bootstrapSchema(resolvedDSN); err != nil {
		return fmt.Errorf("bootstrap application schema: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "schema bootstrap complete source_revision=%s\n", sourceRevision)
	return err
}

func bootstrapSchema(dsn string) (returnErr error) {
	effectiveDSN := dsn
	restoreSQLitePath := func() {}
	if strings.HasPrefix(dsn, "sqlite://") || strings.HasPrefix(dsn, "file:") {
		sqlitePath := strings.TrimPrefix(dsn, "sqlite://")
		if sqlitePath == "" {
			return fmt.Errorf("SQLite DSN path is empty")
		}
		previousSQLitePath := common.SQLitePath
		common.SQLitePath = sqlitePath
		restoreSQLitePath = func() { common.SQLitePath = previousSQLitePath }
		effectiveDSN = "local"
	}
	defer restoreSQLitePath()

	restoreSQLDSN, err := replaceEnvironment("SQL_DSN", effectiveDSN)
	if err != nil {
		return err
	}
	defer restoreSQLDSN()
	restoreNodeRole, err := replaceEnvironment(common.NodeRoleEnvironmentVariable, string(common.NodeRoleLeader))
	if err != nil {
		return err
	}
	defer func() {
		restoreNodeRole()
		_ = common.InitNodeRoleFromEnvironment()
	}()

	previousSchemaManagement := common.SchemaManagementMode
	common.SchemaManagementMode = common.SchemaManagementRuntime
	defer func() { common.SchemaManagementMode = previousSchemaManagement }()
	if err := common.InitNodeRoleFromEnvironment(); err != nil {
		return err
	}
	if err := model.InitDB(); err != nil {
		return err
	}
	sqlDB, err := model.DB.DB()
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, sqlDB.Close())
	}()
	return model.ValidateMainSchemaPrerequisites(model.DB)
}

func replaceEnvironment(name, value string) (func(), error) {
	previous, existed := os.LookupEnv(name)
	if err := os.Setenv(name, value); err != nil {
		return nil, err
	}
	return func() {
		if existed {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	}, nil
}
