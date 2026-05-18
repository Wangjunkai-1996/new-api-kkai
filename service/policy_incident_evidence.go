package service

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	policyIncidentEvidenceDirEnv     = "NEW_API_POLICY_EVIDENCE_DIR"
	defaultPolicyIncidentEvidenceDir = "/var/lib/new-api/policy-evidence"
	policyIncidentEvidenceFilePerm   = 0600
	policyIncidentEvidenceDirPerm    = 0700
)

type policyIncidentEvidenceError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type policyIncidentEvidenceFile struct {
	CaseID                 string                      `json:"case_id"`
	RequestTime            string                      `json:"request_time"`
	RequestID              string                      `json:"request_id"`
	UserID                 int                         `json:"user_id"`
	TokenID                int                         `json:"token_id"`
	TokenName              string                      `json:"token_name"`
	Model                  string                      `json:"model"`
	Path                   string                      `json:"path"`
	RemoteIP               string                      `json:"remote_ip"`
	XForwardedFor          string                      `json:"xff"`
	ChannelID              int                         `json:"channel_id"`
	MultiKeyIndex          int                         `json:"multi_key_index"`
	UpstreamKeyFingerprint string                      `json:"upstream_key_fingerprint"`
	Status                 int                         `json:"status"`
	Error                  policyIncidentEvidenceError `json:"error"`
	BodySHA256             string                      `json:"body_sha256"`
	Body                   string                      `json:"body"`
	BodyRedacted           bool                        `json:"body_redacted"`
}

type policyIncidentEvidenceRecord struct {
	CaseID     string
	Path       string
	SHA256     string
	BodySHA256 string
	Error      string
}

func recordPolicyIncidentEvidence(c *gin.Context, channelError types.ChannelError, classification PolicyIncidentClassification) policyIncidentEvidenceRecord {
	now := policyIncidentRequestTime(c)
	record := policyIncidentEvidenceRecord{
		CaseID: newPolicyIncidentCaseID(now),
	}

	body, err := readPolicyIncidentRequestBody(c)
	if err != nil {
		record.Error = policyIncidentEvidenceErrorString(err, channelError.UsingKey)
		return record
	}
	record.BodySHA256 = policyIncidentHexSHA256(body)

	evidence, err := buildPolicyIncidentEvidenceFile(c, channelError, classification, record.CaseID, now, body)
	if err != nil {
		record.Error = policyIncidentEvidenceErrorString(err, channelError.UsingKey)
		return record
	}

	path, evidenceSHA256, err := writePolicyIncidentEvidenceFile(record.CaseID, evidence)
	if err != nil {
		record.Error = policyIncidentEvidenceErrorString(err, channelError.UsingKey)
		return record
	}

	record.Path = path
	record.SHA256 = evidenceSHA256
	return record
}

func (record policyIncidentEvidenceRecord) Metadata() map[string]any {
	metadata := map[string]any{
		"case_id":              record.CaseID,
		"evidence_body_sha256": record.BodySHA256,
	}
	if record.Path != "" {
		metadata["evidence_path"] = record.Path
	}
	if record.SHA256 != "" {
		metadata["evidence_sha256"] = record.SHA256
	}
	if record.Error != "" {
		metadata["evidence_error"] = record.Error
	}
	return metadata
}

func policyIncidentRequestTime(c *gin.Context) time.Time {
	if c != nil {
		if startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime); !startTime.IsZero() {
			return startTime
		}
	}
	return time.Now()
}

func newPolicyIncidentCaseID(now time.Time) string {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", now.UnixNano(), err.Error())))
		return fmt.Sprintf("policy-%d-%s", now.UnixMilli(), hex.EncodeToString(sum[:8]))
	}
	return fmt.Sprintf("policy-%d-%s", now.UnixMilli(), hex.EncodeToString(randomBytes))
}

func readPolicyIncidentRequestBody(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(storage)
	return body, nil
}

func buildPolicyIncidentEvidenceFile(
	c *gin.Context,
	channelError types.ChannelError,
	classification PolicyIncidentClassification,
	caseID string,
	requestTime time.Time,
	body []byte,
) ([]byte, error) {
	bodyText, bodyRedacted := redactPolicyIncidentEvidenceText(string(body), c, channelError.UsingKey)
	payload := policyIncidentEvidenceFile{
		CaseID:                 caseID,
		RequestTime:            requestTime.UTC().Format(time.RFC3339Nano),
		RequestID:              c.GetString(common.RequestIdKey),
		UserID:                 c.GetInt("id"),
		TokenID:                c.GetInt("token_id"),
		TokenName:              c.GetString("token_name"),
		Model:                  c.GetString("original_model"),
		ChannelID:              channelError.ChannelId,
		MultiKeyIndex:          common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
		UpstreamKeyFingerprint: model.FingerprintPolicyIncidentUpstreamKey(channelError.UsingKey),
		Status:                 classification.StatusCode,
		Error: policyIncidentEvidenceError{
			Code:    classification.ErrorCode,
			Message: redactPolicyIncidentMessage(classification.ErrorMessage, channelError.UsingKey),
		},
		BodySHA256:   policyIncidentHexSHA256(body),
		Body:         bodyText,
		BodyRedacted: bodyRedacted,
	}
	if c.Request != nil {
		payload.RemoteIP = policyIncidentRemoteIP(c)
		payload.XForwardedFor = c.Request.Header.Get("X-Forwarded-For")
		if c.Request.URL != nil {
			payload.Path = redactPolicyIncidentMessage(c.Request.URL.Path, channelError.UsingKey)
		}
	}
	return common.Marshal(payload)
}

func policyIncidentRemoteIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	remoteAddr := strings.TrimSpace(c.Request.RemoteAddr)
	if remoteAddr == "" {
		return c.ClientIP()
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func writePolicyIncidentEvidenceFile(caseID string, payload []byte) (string, string, error) {
	gzipPayload, err := gzipPolicyIncidentEvidence(payload)
	if err != nil {
		return "", "", err
	}

	dir := policyIncidentEvidenceDir()
	if err := os.MkdirAll(dir, policyIncidentEvidenceDirPerm); err != nil {
		return "", "", err
	}
	if err := os.Chmod(dir, policyIncidentEvidenceDirPerm); err != nil {
		return "", "", err
	}

	path := filepath.Join(dir, caseID+".json.gz")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, policyIncidentEvidenceFilePerm)
	if err != nil {
		return "", "", err
	}

	_, writeErr := file.Write(gzipPayload)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return "", "", writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", "", closeErr
	}
	if err := os.Chmod(path, policyIncidentEvidenceFilePerm); err != nil {
		return "", "", err
	}

	return path, policyIncidentHexSHA256(gzipPayload), nil
}

func policyIncidentEvidenceDir() string {
	dir := strings.TrimSpace(os.Getenv(policyIncidentEvidenceDirEnv))
	if dir == "" {
		dir = defaultPolicyIncidentEvidenceDir
	}
	return filepath.Clean(dir)
}

func gzipPolicyIncidentEvidence(payload []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func policyIncidentHexSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func redactPolicyIncidentEvidenceText(text string, c *gin.Context, upstreamKey string) (string, bool) {
	redacted := text
	for _, secret := range policyIncidentEvidenceSecrets(c, upstreamKey) {
		redacted = strings.ReplaceAll(redacted, secret, model.PolicyIncidentMetadataRedacted)
	}
	return redacted, redacted != text
}

func policyIncidentEvidenceSecrets(c *gin.Context, upstreamKey string) []string {
	secrets := make([]string, 0, 8)
	addSecret := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range secrets {
			if existing == value {
				return
			}
		}
		secrets = append(secrets, value)
	}

	for _, variant := range policyIncidentKeyRedactionVariants(upstreamKey) {
		addSecret(variant)
	}
	if c == nil || c.Request == nil {
		return secrets
	}

	authorization := c.Request.Header.Get("Authorization")
	addSecret(authorization)
	authFields := strings.Fields(authorization)
	if len(authFields) == 2 && strings.EqualFold(authFields[0], "Bearer") {
		addSecret(authFields[1])
		addSecret("Bearer " + authFields[1])
		addSecret("bearer " + authFields[1])
	}

	tokenKey := c.GetString("token_key")
	addSecret(tokenKey)
	if tokenKey != "" {
		addSecret("sk-" + tokenKey)
	}
	return secrets
}

func policyIncidentEvidenceErrorString(err error, upstreamKey string) string {
	if err == nil {
		return ""
	}
	return redactPolicyIncidentMessage(err.Error(), upstreamKey)
}
