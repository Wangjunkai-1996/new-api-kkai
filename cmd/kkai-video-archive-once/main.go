package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	archiveOnceWorkerID       = "kkai-video-archive-once"
	archiveOnceDefaultTimeout = 30 * time.Minute
	archiveOnceMaximumTimeout = 2 * time.Hour
)

type archiveOnceOptions struct {
	TaskID         int64
	GenerationID   int64
	UserID         int
	ExternalTaskID string
	Execute        bool
	Timeout        time.Duration
}

type archiveOnceOutput struct {
	TaskID       int64  `json:"task_id"`
	GenerationID int64  `json:"generation_id"`
	UserID       int    `json:"user_id"`
	AssetID      int64  `json:"asset_id"`
	Stage        string `json:"stage"`
	DryRun       bool   `json:"dry_run"`
}

type archiveOnceOperation func(context.Context, archiveOnceOptions) (archiveOnceOutput, error)

func silenceArchiveOnceDependencyLogs() func() {
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultWriter
	previousErrorWriter := gin.DefaultErrorWriter
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	common.LogWriterMu.Unlock()

	previousGORMLogger := gormlogger.Default
	gormlogger.Default = previousGORMLogger.LogMode(gormlogger.Silent)
	return func() {
		gormlogger.Default = previousGORMLogger
		common.LogWriterMu.Lock()
		gin.DefaultWriter = previousWriter
		gin.DefaultErrorWriter = previousErrorWriter
		common.LogWriterMu.Unlock()
	}
}

func parseArchiveOnceOptions(arguments []string, _ io.Writer) (archiveOnceOptions, error) {
	options := archiveOnceOptions{Timeout: archiveOnceDefaultTimeout}
	var userID int64
	var explicitDryRun bool
	flags := flag.NewFlagSet("kkai-video-archive-once", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Int64Var(&options.TaskID, "task-id", 0, "exact internal task ID")
	flags.Int64Var(&options.GenerationID, "generation-id", 0, "exact video generation ID")
	flags.Int64Var(&userID, "user-id", 0, "exact task owner user ID")
	flags.StringVar(&options.ExternalTaskID, "external-task-id", "", "exact external task ID")
	flags.BoolVar(&explicitDryRun, "dry-run", false, "preview the exact task without writing")
	flags.BoolVar(&options.Execute, "execute", false, "execute the exact task archive pipeline")
	flags.DurationVar(&options.Timeout, "timeout", archiveOnceDefaultTimeout, "overall command timeout")
	if err := flags.Parse(arguments); err != nil {
		return archiveOnceOptions{}, err
	}
	if flags.NArg() != 0 {
		return archiveOnceOptions{}, errors.New("positional arguments are not supported")
	}
	if explicitDryRun && options.Execute {
		return archiveOnceOptions{}, errors.New("--dry-run and --execute cannot be combined")
	}
	if options.TaskID <= 0 {
		return archiveOnceOptions{}, errors.New("--task-id must be positive")
	}
	if options.GenerationID <= 0 {
		return archiveOnceOptions{}, errors.New("--generation-id must be positive")
	}
	if userID <= 0 || uint64(userID) > uint64(math.MaxInt) {
		return archiveOnceOptions{}, errors.New("--user-id must be a positive platform integer")
	}
	if options.ExternalTaskID == "" || len(options.ExternalTaskID) > 191 ||
		strings.TrimSpace(options.ExternalTaskID) != options.ExternalTaskID {
		return archiveOnceOptions{}, errors.New("--external-task-id must be 1 to 191 bytes with no surrounding whitespace")
	}
	if options.Timeout <= 0 || options.Timeout > archiveOnceMaximumTimeout {
		return archiveOnceOptions{}, errors.New("--timeout must be greater than zero and at most 2h")
	}
	options.UserID = int(userID)
	return options, nil
}

func runArchiveOnceCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	operation archiveOnceOperation,
) error {
	options, err := parseArchiveOnceOptions(arguments, stderr)
	if err != nil {
		return err
	}
	if operation == nil {
		return errors.New("archive operation is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	restoreLogs := silenceArchiveOnceDependencyLogs()
	defer restoreLogs()
	result, err := operation(ctx, options)
	if err != nil {
		return err
	}
	if result.TaskID != options.TaskID || result.GenerationID != options.GenerationID ||
		result.UserID != options.UserID || result.AssetID < 0 || strings.TrimSpace(result.Stage) == "" ||
		(options.Execute && (result.DryRun || result.AssetID == 0)) || (!options.Execute && !result.DryRun) {
		return errors.New("archive operation returned an invalid result")
	}
	encoded, err := common.Marshal(result)
	if err != nil {
		return errors.New("encode archive result")
	}
	_, err = fmt.Fprintln(stdout, string(encoded))
	return err
}

func openArchiveOnceDatabase(ctx context.Context, execute bool) (*gorm.DB, error) {
	if common.SchemaManagementMode != common.SchemaManagementExternal {
		return nil, errors.New("archive executor requires external schema management")
	}
	if strings.TrimSpace(os.Getenv("SQL_DSN")) == "" {
		return nil, errors.New("SQL_DSN is required")
	}
	if err := common.InitNodeRoleFromEnvironment(); err != nil {
		return nil, err
	}
	if execute && common.CurrentNodeRole() != common.NodeRoleServing {
		return nil, errors.New("archive execution requires a serving node")
	}
	if err := model.InitDB(); err != nil {
		return nil, err
	}
	if err := kkaimigrate.CheckRequired(ctx, model.DB); err != nil {
		if sqlDB, dbErr := model.DB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	return model.DB, nil
}

func performArchiveOnce(ctx context.Context, options archiveOnceOptions) (archiveOnceOutput, error) {
	db, err := openArchiveOnceDatabase(ctx, options.Execute)
	if err != nil {
		return archiveOnceOutput{}, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return archiveOnceOutput{}, err
	}
	defer sqlDB.Close()
	model.InitOptionMap()
	if err := model.SyncOptionsOnce(); err != nil {
		return archiveOnceOutput{}, err
	}

	input := service.VideoTaskArchiveOnceInput{
		TaskID:                 options.TaskID,
		GenerationID:           options.GenerationID,
		ExpectedUserID:         options.UserID,
		ExpectedExternalTaskID: options.ExternalTaskID,
	}
	if !options.Execute {
		result, err := service.PreviewVideoTaskArchiveOnce(ctx, db, input)
		if err != nil {
			return archiveOnceOutput{}, err
		}
		return archiveOnceOutput{
			TaskID: result.TaskID, GenerationID: result.GenerationID, UserID: options.UserID,
			AssetID: result.AssetID, Stage: fmt.Sprint(result.Stage), DryRun: true,
		}, nil
	}
	service.InitHttpClient()
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		return relay.GetTaskAdaptor(platform)
	}
	store, err := service.NewR2VideoAssetStoreFromEnvironment(ctx)
	if err != nil {
		return archiveOnceOutput{}, err
	}
	media, err := service.NewPinnedFFmpegVideoMediaProcessorFromEnvironment(ctx)
	if err != nil {
		return archiveOnceOutput{}, err
	}
	tempDir := strings.TrimSpace(os.Getenv("VIDEO_STUDIO_TEMP_DIR"))
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	executor, err := service.NewVideoTaskArchiveOnceExecutor(
		db, store, media, service.NewHTTPVideoArchiveFetcher(tempDir), tempDir, archiveOnceWorkerID,
	)
	if err != nil {
		return archiveOnceOutput{}, err
	}
	result, err := executor.Execute(ctx, input)
	if err != nil {
		return archiveOnceOutput{}, err
	}
	return archiveOnceOutput{
		TaskID: result.TaskID, GenerationID: result.GenerationID, UserID: options.UserID,
		AssetID: result.AssetID, Stage: fmt.Sprint(result.Stage), DryRun: false,
	}, nil
}

func main() {
	err := runArchiveOnceCommand(context.Background(), os.Args[1:], os.Stdout, os.Stderr, performArchiveOnce)
	if err == nil {
		return
	}
	status := "failed"
	if errors.Is(err, service.ErrVideoTaskArchiveOnceMismatch) {
		status = "coordinate_mismatch"
	}
	_, _ = fmt.Fprintf(os.Stderr, "kkai-video-archive-once: %s\n", status)
	os.Exit(1)
}
