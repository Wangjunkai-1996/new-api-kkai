// kkai-video-studio-config performs the reviewed, one-time multimodal rollout.
// It never starts the application, background jobs, or schema migrations.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm/logger"
)

const protocol = "video-studio-config-v1"

type manifest struct {
	Protocol               string        `json:"protocol"`
	Operator               string        `json:"operator"`
	Action                 string        `json:"action"`
	Profiles               []profilePlan `json:"profiles,omitempty"`
	ExpectedProfilesSHA256 string        `json:"expected_profiles_sha256,omitempty"`
	Switch                 *switchPlan   `json:"switch,omitempty"`
}

type profilePlan struct {
	ID              int64                  `json:"id"`
	Model           string                 `json:"model"`
	ExpectedVersion int                    `json:"expected_version"`
	ExpectedSHA256  string                 `json:"expected_sha256"`
	Specification   service.VideoModelSpec `json:"specification"`
}

type switchPlan struct {
	Expected string `json:"expected"` // absent, false, or true
	Value    *bool  `json:"value"`
}

type result struct {
	Protocol       string         `json:"protocol"`
	Mode           string         `json:"mode"`
	Action         string         `json:"action,omitempty"`
	Operator       string         `json:"operator,omitempty"`
	ManifestSHA256 string         `json:"manifest_sha256,omitempty"`
	Version        string         `json:"version"`
	OperatorUID    int            `json:"operator_uid"`
	ObservedAt     string         `json:"observed_at"`
	Profiles       []profileState `json:"profiles,omitempty"`
	ProfilesSHA256 string         `json:"profiles_sha256,omitempty"`
	Switch         string         `json:"switch,omitempty"`
	Updated        int            `json:"updated"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		// All returned errors are fixed messages; never echo driver/DSN errors.
		fmt.Fprintln(os.Stderr, "kkai-video-studio-config:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("kkai-video-studio-config", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "reviewed manifest")
	digest := flags.String("manifest-sha256", "", "exact reviewed manifest SHA256")
	inspect := flags.Bool("inspect", false, "read the eleven profiles and switch")
	apply := flags.Bool("apply", false, "apply the reviewed action")
	dsnStdin := flags.Bool("dsn-stdin", false, "read the DSN only from stdin")
	auditPath := flags.String("audit-file", "", "new root-only audit file required for apply")
	timeout := flags.Duration("timeout", 30*time.Second, "database operation timeout; PostgreSQL connection is bounded separately at 5s")
	if flags.Parse(args) != nil || flags.NArg() != 0 || !*dsnStdin || *timeout <= 0 || *timeout > 2*time.Minute ||
		(*inspect && (*apply || *manifestPath != "" || *digest != "")) || (!*inspect && (*manifestPath == "" || !validSHA256(*digest))) {
		return errors.New("require --dsn-stdin and either --inspect or --manifest FILE --manifest-sha256 SHA; timeout must be at most 2m")
	}
	if common.SchemaManagementMode != common.SchemaManagementExternal {
		return errors.New("requires an immutable external-schema build")
	}
	if os.Getenv("SQL_DSN") != "" || os.Getenv("KKAI_MIGRATION_DSN") != "" {
		return errors.New("DSN environment variables must be unset; use stdin")
	}
	var input manifest
	if !*inspect {
		file, err := os.Open(*manifestPath)
		if err != nil {
			return errors.New("cannot open manifest")
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		file.Close()
		if readErr != nil || len(raw) > 1<<20 || fmt.Sprintf("%x", sha256.Sum256(raw)) != *digest || common.UnmarshalStrict(raw, &input) != nil {
			return errors.New("manifest is invalid or does not match its reviewed SHA256")
		}
		if err := validateManifest(input); err != nil {
			return err
		}
	}
	var audit *os.File
	if *apply {
		if os.Geteuid() != 0 || *auditPath == "" {
			return errors.New("apply requires the host-authorized root operator and a new audit file")
		}
		var err error
		audit, err = os.OpenFile(*auditPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("cannot create a new audit file; inspect previous results before retrying")
		}
		defer audit.Close()
	}
	dsn, err := kkaischemacli.ResolveDSN("", true, stdin, "database DSN required on stdin")
	if err != nil {
		return errors.New("invalid DSN input")
	}
	// The production driver opens its connection before the operation context.
	// Bound that connection as well, without printing or persisting the DSN.
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		connectionURL, parseErr := url.Parse(dsn)
		if parseErr != nil {
			return errors.New("invalid PostgreSQL DSN input")
		}
		query := connectionURL.Query()
		query.Set("connect_timeout", "5")
		connectionURL.RawQuery = query.Encode()
		dsn = connectionURL.String()
	}
	previousLogger, previousWriter, previousErrorWriter := logger.Default, gin.DefaultWriter, gin.DefaultErrorWriter
	logger.Default, gin.DefaultWriter, gin.DefaultErrorWriter = logger.Default.LogMode(logger.Silent), io.Discard, io.Discard
	defer func() {
		logger.Default, gin.DefaultWriter, gin.DefaultErrorWriter = previousLogger, previousWriter, previousErrorWriter
	}()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	db, err := kkaischemacli.OpenDatabase(dsn)
	if err != nil {
		return errors.New("database connection failed")
	}
	connection, err := db.DB()
	if err != nil {
		return errors.New("database connection unavailable")
	}
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	if err := kkaimigrate.CheckRequired(ctx, db); err != nil {
		return errors.New("database does not satisfy this exact image's schema contract")
	}
	previousDialect := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseType(db.Dialector.Name()))
	defer common.SetMainDatabaseType(previousDialect)
	output, err := operate(ctx, db, input, *inspect, *apply, *digest, audit)
	if err != nil {
		return err
	}
	encoded, err := common.Marshal(output)
	if err != nil {
		return errors.New("cannot encode result; inspect the audit and database before retrying")
	}
	if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
		return errors.New("cannot write result; inspect the audit and database before retrying")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	return strings.Trim(value, "0123456789abcdef") == ""
}
