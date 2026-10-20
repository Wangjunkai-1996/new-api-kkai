package relay

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTenantResponsesLimitCatalog(t *testing.T) {
	t.Helper()
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(model.MembershipModels()...))
	_, err := model.PublishMembershipCatalog(model.MembershipCatalog{ModelLimits: map[string]model.MembershipModelLimit{
		"tenant-text":    {MaxInputTokens: 100, MaxOutputTokens: 40, Evidence: "provider aggregate request bounds"},
		"missing-input":  {MaxOutputTokens: 40, Evidence: "output bound only"},
		"missing-output": {MaxInputTokens: 100, Evidence: "input bound only"},
		"oversized":      {MaxInputTokens: 100, MaxOutputTokens: math.MaxInt32, Evidence: "provider aggregate request bounds"},
	}}, 0, 1)
	require.NoError(t, err)
}

func TestPrepareTenantResponsesOutputLimit(t *testing.T) {
	setupTenantResponsesLimitCatalog(t)
	for _, tc := range []struct {
		name, body, model       string
		tenant                  int64
		want                    uint
		wantNil, wantErr, fixed bool
	}{
		{name: "omitted", body: `{}`, model: "tenant-text", tenant: 7, want: 40},
		{name: "null", body: `{"max_output_tokens":null}`, model: "tenant-text", tenant: 7, want: 40},
		{name: "explicit zero", body: `{"max_output_tokens":0}`, model: "tenant-text", tenant: 7},
		{name: "explicit cap", body: `{"max_output_tokens":10}`, model: "tenant-text", tenant: 7, want: 10},
		{name: "platform unchanged", body: `{}`, model: "tenant-text", wantNil: true},
		{name: "fixed price needs no bound", body: `{}`, model: "unknown", tenant: 7, fixed: true, wantNil: true},
		{name: "unknown bound", body: `{}`, model: "unknown", tenant: 7, wantNil: true, wantErr: true},
		{name: "missing input", body: `{}`, model: "missing-input", tenant: 7, wantNil: true, wantErr: true},
		{name: "missing output", body: `{}`, model: "missing-output", tenant: 7, wantNil: true, wantErr: true},
		{name: "oversized output", body: `{}`, model: "oversized", tenant: 7, wantNil: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.Unmarshal([]byte(tc.body), request))
			original := request.MaxOutputTokens
			info := &relaycommon.RelayInfo{TenantID: tc.tenant, OriginModelName: "client-alias", BillingModelName: tc.model, RelayMode: relayconstant.RelayModeResponses, Request: request}
			price := hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}, UsePrice: tc.fixed, ModelPrice: 1}
			meta := request.GetTokenCountMeta()
			err := prepareTenantResponsesOutputLimit(info, &price, meta)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.wantNil {
				assert.Nil(t, request.MaxOutputTokens)
				if tc.fixed {
					require.NoError(t, service.PrepareTenantBudget(info, &price, 1, meta))
					assert.False(t, info.TenantBudget.RequiresOutputBound)
				}
				return
			}
			require.NotNil(t, request.MaxOutputTokens)
			assert.Equal(t, tc.want, *request.MaxOutputTokens)
			if original != nil {
				assert.Same(t, original, request.MaxOutputTokens, "explicit limits remain client-controlled")
			}
			assert.EqualValues(t, tc.want, meta.MaxTokens, "budget metadata matches the forwarded limit")
			err = service.PrepareTenantBudget(info, &price, 1, meta)
			if tc.want == 0 {
				require.ErrorContains(t, err, "max_tokens")
				assert.Nil(t, info.TenantBudget)
				return
			}
			require.NoError(t, err)
			assert.EqualValues(t, tc.want, info.TenantBudget.MaxOutputTokens)
			assert.Equal(t, 140, price.QuotaToPreConsume, "authorization retains the full provider aggregate bound")
		})
	}
}
