package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
)

var sourceRevision string

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
}

func run(arguments []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("kkai-schema-observe", flag.ContinueOnError)
	flags.SetOutput(stdout)
	var (
		dsn            string
		dsnFromStdin   bool
		current        bool
		checkUpstream  bool
		jsonOutput     bool
		describeSource string
		timeout        time.Duration
	)
	flags.StringVar(
		&dsn,
		"dsn",
		kkaischemacli.FirstNonEmpty(os.Getenv("KKAI_SCHEMA_OBSERVE_DSN"), os.Getenv("SQL_DSN")),
		"database DSN",
	)
	flags.BoolVar(&dsnFromStdin, "dsn-stdin", false, "read one database DSN from stdin")
	flags.BoolVar(&current, "current", false, "read the validated current KKAI schema state")
	flags.BoolVar(&checkUpstream, "check-upstream-baseline", false, "read-only exact check of the upstream database schema")
	flags.BoolVar(&jsonOutput, "json", false, "emit canonical machine-readable JSON")
	flags.StringVar(&describeSource, "source-revision", sourceRevision, "source revision for upstream adoption evidence")
	flags.DurationVar(&timeout, "timeout", time.Minute, "overall observation timeout")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("schema observer does not accept positional arguments")
	}
	if observationModeCount(current, checkUpstream) != 1 {
		return fmt.Errorf("exactly one schema observation mode is required")
	}
	if !jsonOutput {
		return fmt.Errorf("schema observation requires --json")
	}

	resolvedDSN, err := kkaischemacli.ResolveDSN(
		dsn,
		dsnFromStdin,
		stdin,
		"KKAI_SCHEMA_OBSERVE_DSN, SQL_DSN, --dsn, or --dsn-stdin is required",
	)
	if err != nil {
		return err
	}
	db, err := kkaischemacli.OpenDatabase(resolvedDSN)
	if err != nil {
		return fmt.Errorf("failed to open schema observation database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to access schema observation database")
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if current {
		observation, err := kkaimigrate.Observe(ctx, db)
		if err != nil {
			return fmt.Errorf("KKAI schema observation failed: %w", err)
		}
		return writeJSON(stdout, observation)
	}

	adoption, err := kkaimigrate.ObserveUpstreamSchema(ctx, db, describeSource)
	if err != nil {
		return fmt.Errorf("upstream schema baseline observation failed: %w", err)
	}
	encoded, err := adoption.CanonicalJSON()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
		return err
	}
	if !adoption.Ready {
		return fmt.Errorf("upstream schema baseline is incomplete")
	}
	return nil
}

func observationModeCount(modes ...bool) int {
	count := 0
	for _, enabled := range modes {
		if enabled {
			count++
		}
	}
	return count
}

func writeJSON(writer io.Writer, value any) error {
	encoded, err := common.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(writer, string(encoded))
	return err
}
