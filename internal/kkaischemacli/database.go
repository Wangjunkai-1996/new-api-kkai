package kkaischemacli

import (
	"fmt"
	"io"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const maximumDSNBytes = 8192

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ResolveDSN(explicitDSN string, fromStdin bool, reader io.Reader, missingSource string) (string, error) {
	explicitDSN = strings.TrimSpace(explicitDSN)
	if !fromStdin {
		if explicitDSN == "" {
			return "", fmt.Errorf("%s", missingSource)
		}
		return explicitDSN, nil
	}
	if explicitDSN != "" {
		return "", fmt.Errorf("--dsn-stdin cannot be combined with --dsn or a DSN environment variable")
	}

	rawDSN, err := io.ReadAll(io.LimitReader(reader, maximumDSNBytes+1))
	if err != nil {
		return "", fmt.Errorf("read database DSN from stdin: %w", err)
	}
	if len(rawDSN) > maximumDSNBytes {
		return "", fmt.Errorf("database DSN exceeds %d bytes", maximumDSNBytes)
	}

	dsnInput := string(rawDSN)
	switch {
	case strings.HasSuffix(dsnInput, "\r\n"):
		dsnInput = strings.TrimSuffix(dsnInput, "\r\n")
	case strings.HasSuffix(dsnInput, "\n"):
		dsnInput = strings.TrimSuffix(dsnInput, "\n")
	}
	if strings.ContainsAny(dsnInput, "\r\n") {
		return "", fmt.Errorf("database DSN from stdin must contain exactly one line")
	}

	dsn := strings.TrimSpace(dsnInput)
	if dsn == "" {
		return "", fmt.Errorf("database DSN from stdin is empty")
	}
	return dsn, nil
}

func OpenDatabase(dsn string) (*gorm.DB, error) {
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	case strings.HasPrefix(dsn, "sqlite://"):
		return gorm.Open(sqlite.Open(strings.TrimPrefix(dsn, "sqlite://")), &gorm.Config{})
	case strings.HasPrefix(dsn, "file:"):
		return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	default:
		if !strings.Contains(dsn, "parseTime=") {
			separator := "?"
			if strings.Contains(dsn, "?") {
				separator = "&"
			}
			dsn += separator + "parseTime=true"
		}
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})
	}
}
