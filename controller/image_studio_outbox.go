package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type imageOutboxRedriveRequest struct {
	RedriveKey string `json:"redrive_key" binding:"required"`
}

func AdminRedriveImageStudioOutboxEvent(c *gin.Context) {
	eventID, err := imageStudioID(c)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	var request imageOutboxRedriveRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondImageStudioError(c, service.ErrInvalidImageOutboxEvent)
		return
	}
	event, applied, err := service.RedriveImageOutboxDeadEvent(
		c.Request.Context(), model.DB, eventID, request.RedriveKey,
		fmt.Sprintf("admin:%d", c.GetInt("id")), time.Now(),
	)
	if err != nil {
		respondImageStudioError(c, err)
		return
	}
	recordManageAudit(c, "image.outbox.redrive", map[string]interface{}{
		"event_id": event.ID, "topic": event.Topic, "aggregate_id": event.AggregateID,
		"redrive_key": request.RedriveKey, "applied": applied,
	})
	imageStudioSuccess(c, http.StatusOK, gin.H{
		"id": event.ID, "topic": event.Topic, "aggregate_id": event.AggregateID,
		"status": event.Status, "attempts": event.Attempts,
		"available_at": event.AvailableAt, "applied": applied,
	})
}
