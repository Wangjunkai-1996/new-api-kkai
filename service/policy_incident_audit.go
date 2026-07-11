package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

type policyIncidentAuditMetadata struct {
	CaseID                   string
	RequestBodySHA256        string
	RequestBodyBytes         int64
	ClientTokenActionAllowed bool
}

func preparePolicyIncidentAuditMetadata(c *gin.Context, clientTokenActionAllowed bool) policyIncidentAuditMetadata {
	metadata := policyIncidentAuditMetadata{
		CaseID:                   newPolicyIncidentCaseID(policyIncidentRequestTime(c)),
		ClientTokenActionAllowed: clientTokenActionAllowed,
	}
	if c != nil {
		c.Set(PolicyIncidentCaseIDContextKey, metadata.CaseID)
	}

	digest, size, err := readPolicyIncidentRequestBodyDigest(c)
	if err != nil {
		return metadata
	}
	metadata.RequestBodySHA256 = digest
	metadata.RequestBodyBytes = size
	return metadata
}

func (metadata policyIncidentAuditMetadata) Map() map[string]any {
	values := map[string]any{
		"case_id":                     metadata.CaseID,
		"client_token_action_allowed": metadata.ClientTokenActionAllowed,
	}
	if metadata.RequestBodySHA256 != "" {
		values["request_body_sha256"] = metadata.RequestBodySHA256
		values["request_body_bytes"] = metadata.RequestBodyBytes
	}
	return values
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

func readPolicyIncidentRequestBodyDigest(c *gin.Context) (string, int64, error) {
	hasher := sha256.New()
	if c == nil {
		return hex.EncodeToString(hasher.Sum(nil)), 0, nil
	}
	if _, exists := c.Get(common.KeyBodyStorage); !exists && (c.Request == nil || c.Request.Body == nil) {
		return hex.EncodeToString(hasher.Sum(nil)), 0, nil
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", 0, err
	}
	if _, err = storage.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	bodySize, copyErr := io.CopyBuffer(hasher, storage, make([]byte, 32*1024))
	_, seekErr := storage.Seek(0, io.SeekStart)
	if c.Request != nil {
		c.Request.Body = io.NopCloser(storage)
	}
	if copyErr != nil {
		return "", 0, copyErr
	}
	if seekErr != nil {
		return "", 0, seekErr
	}
	return hex.EncodeToString(hasher.Sum(nil)), bodySize, nil
}
