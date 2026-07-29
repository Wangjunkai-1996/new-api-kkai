package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func exactArchiveArguments() []string {
	return []string{
		"--task-id", "101",
		"--generation-id", "3",
		"--user-id", "1",
		"--external-task-id", "task_exact_archive",
	}
}

func TestParseArchiveOnceOptionsDefaultsToDryRun(t *testing.T) {
	options, err := parseArchiveOnceOptions(exactArchiveArguments(), &bytes.Buffer{})
	require.NoError(t, err)
	require.EqualValues(t, 101, options.TaskID)
	require.EqualValues(t, 3, options.GenerationID)
	require.Equal(t, 1, options.UserID)
	require.Equal(t, "task_exact_archive", options.ExternalTaskID)
	require.False(t, options.Execute)
	require.Equal(t, archiveOnceDefaultTimeout, options.Timeout)
}

func TestParseArchiveOnceOptionsRequiresEveryExactCoordinate(t *testing.T) {
	tests := map[string][]string{
		"task":       {"--generation-id", "3", "--user-id", "1", "--external-task-id", "task_exact_archive"},
		"generation": {"--task-id", "101", "--user-id", "1", "--external-task-id", "task_exact_archive"},
		"user":       {"--task-id", "101", "--generation-id", "3", "--external-task-id", "task_exact_archive"},
		"external":   {"--task-id", "101", "--generation-id", "3", "--user-id", "1"},
	}
	for name, arguments := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseArchiveOnceOptions(arguments, &bytes.Buffer{})
			require.Error(t, err)
		})
	}
}

func TestParseArchiveOnceOptionsRequiresExplicitExecute(t *testing.T) {
	arguments := append(exactArchiveArguments(), "--execute")
	options, err := parseArchiveOnceOptions(arguments, &bytes.Buffer{})
	require.NoError(t, err)
	require.True(t, options.Execute)

	arguments = append(arguments, "--dry-run")
	_, err = parseArchiveOnceOptions(arguments, &bytes.Buffer{})
	require.ErrorContains(t, err, "cannot be combined")
}

func TestRunArchiveOnceCommandOutputsOnlyIDsAndState(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runArchiveOnceCommand(
		context.Background(), exactArchiveArguments(), &stdout, &stderr,
		func(_ context.Context, options archiveOnceOptions) (archiveOnceOutput, error) {
			require.False(t, options.Execute)
			return archiveOnceOutput{
				TaskID: 101, GenerationID: 3, UserID: 1, AssetID: 0,
				Stage: "awaiting_archive", DryRun: true,
			}, nil
		},
	)
	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.JSONEq(t, `{
		"task_id": 101,
		"generation_id": 3,
		"user_id": 1,
		"asset_id": 0,
		"stage": "awaiting_archive",
		"dry_run": true
	}`, stdout.String())
	for _, forbidden := range []string{"external_task_id", "url", "object", "key", "secret", "credential"} {
		require.NotContains(t, strings.ToLower(stdout.String()), forbidden)
	}
}

func TestRunArchiveOnceCommandRejectsMismatchedOperationResult(t *testing.T) {
	err := runArchiveOnceCommand(
		context.Background(), exactArchiveArguments(), &bytes.Buffer{}, &bytes.Buffer{},
		func(context.Context, archiveOnceOptions) (archiveOnceOutput, error) {
			return archiveOnceOutput{
				TaskID: 999, GenerationID: 3, UserID: 1,
				Stage: "awaiting_archive", DryRun: true,
			}, nil
		},
	)
	require.ErrorContains(t, err, "invalid result")
}

func TestRunArchiveOnceCommandDoesNotEchoMalformedFlagValue(t *testing.T) {
	var stderr bytes.Buffer
	err := runArchiveOnceCommand(
		context.Background(),
		[]string{
			"--task-id", "https://secret.example/object-key?credential=hidden",
			"--generation-id", "3",
			"--user-id", "1",
			"--external-task-id", "task_exact_archive",
		},
		&bytes.Buffer{}, &stderr,
		func(context.Context, archiveOnceOptions) (archiveOnceOutput, error) {
			t.Fatal("operation must not run after flag parsing fails")
			return archiveOnceOutput{}, nil
		},
	)
	require.Error(t, err)
	require.Empty(t, stderr.String())
}

func TestRunArchiveOnceCommandSuppressesDependencyLogs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runArchiveOnceCommand(
		context.Background(), exactArchiveArguments(), &stdout, &stderr,
		func(context.Context, archiveOnceOptions) (archiveOnceOutput, error) {
			common.SysLog("https://secret.example/object-key")
			common.SysError("credential=hidden")
			return archiveOnceOutput{
				TaskID: 101, GenerationID: 3, UserID: 1,
				Stage: "awaiting_archive", DryRun: true,
			}, nil
		},
	)
	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.NotContains(t, stdout.String(), "secret.example")
	require.NotContains(t, stdout.String(), "credential")
	require.JSONEq(t, `{
		"task_id": 101,
		"generation_id": 3,
		"user_id": 1,
		"asset_id": 0,
		"stage": "awaiting_archive",
		"dry_run": true
	}`, stdout.String())
}
