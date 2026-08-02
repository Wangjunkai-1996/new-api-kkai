package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const imageAccountingRetryInterval = 5 * time.Second

type ImageGenerationAccountingHandler struct{}

func (ImageGenerationAccountingHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	payload := model.ImageGenerationAccountingPayload{}
	if event.Topic != model.KKAIOutboxTopicImageAccounting ||
		common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.GenerationID <= 0 ||
		payload.TargetQuota < 0 || payload.LogParams.Quota != payload.TargetQuota ||
		event.AggregateID != strconv.FormatInt(payload.GenerationID, 10) {
		return PermanentKKAIOutboxError(errors.New("invalid image accounting payload"))
	}
	_, err := model.RecordImageGenerationAccountingLog(ctx, model.DB, payload)
	if errors.Is(err, model.ErrImageAccountingNotReady) {
		return DeferKKAIOutboxUntil(time.Now().Add(imageAccountingRetryInterval), err)
	}
	if errors.Is(err, model.ErrImageBillingInvalidRequest) || errors.Is(err, model.ErrImageBillingStateConflict) {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid image accounting state: %w", err))
	}
	return err
}
