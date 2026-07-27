package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type videoOutboxRedriveRequest struct {
	RedriveKey string `json:"redrive_key" binding:"required"`
}

type videoOutboxRedriveResponse struct {
	ID          int64  `json:"id"`
	Topic       string `json:"topic"`
	AggregateID string `json:"aggregate_id"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	AvailableAt int64  `json:"available_at"`
	Applied     bool   `json:"applied"`
}

func AdminRedriveVideoStudioOutboxEvent(c *gin.Context) {
	eventID, err := videoStudioID(c)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	var request videoOutboxRedriveRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondVideoStudioError(c, service.ErrInvalidVideoOutboxEvent)
		return
	}

	event, applied, err := service.RedriveVideoOutboxDeadEvent(
		c.Request.Context(),
		model.DB,
		eventID,
		request.RedriveKey,
		fmt.Sprintf("admin:%d", c.GetInt("id")),
		time.Now(),
	)
	if err != nil {
		respondVideoStudioError(c, err)
		return
	}
	recordManageAudit(c, "video.outbox.redrive", map[string]interface{}{
		"event_id":     event.ID,
		"topic":        event.Topic,
		"aggregate_id": event.AggregateID,
		"redrive_key":  request.RedriveKey,
		"applied":      applied,
	})
	videoStudioSuccess(c, http.StatusOK, videoOutboxRedriveResponse{
		ID: event.ID, Topic: event.Topic, AggregateID: event.AggregateID,
		Status: event.Status, Attempts: event.Attempts, AvailableAt: event.AvailableAt,
		Applied: applied,
	})
}
