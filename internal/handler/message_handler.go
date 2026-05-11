package handler

import (
	"strconv"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
)

type MessageHandler struct {
	ms *s.MessageService
}

func NewMessageHandler(ms *s.MessageService) *MessageHandler {
	return &MessageHandler{ms: ms}
}

func (h *MessageHandler) SendMessage(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	var req dto.SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	convID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.BadRequest("Invalid conversation ID"))
		return
	}
	if len(req.Attachments) == 0 {
		msg, err := h.ms.SendMessage(c, convID, user.ID, req.Type, req.Content, req.ParentID)
		if err != nil {
			_ = c.Error(err)
			return
		}
		r.OK(c, msg, "Send msg successfully")
		return
	}
	msg, err := h.ms.SendMessageWithAttachment(c, convID, user.ID, req.Type, req.Content, req.ParentID, h.toAttachmentDomain(req.Attachments))
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.OK(c, msg, "Send msg with attachment successfully")
}

func (h *MessageHandler) ListMessages(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	convID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.BadRequest("Invalid conversation ID"))
		return
	}
	cursor := c.Query("cursor")
	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.Atoi(limitStr)
	msgs, err := h.ms.ListMessages(c, user.ID, convID, &cursor, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.Paginated(c, msgs, &r.Pagination{
		Limit:      limit,
		HasMore:    msgs.HasMore,
		NextCursor: *msgs.NextCursor,
	}, "List messages successfully")
}

func (h *MessageHandler) MarkAsRead(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	convID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.BadRequest("Invalid conversation ID"))
		return
	}
	var req dto.MarkAsReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	if err := h.ms.MarkAsRead(c, convID, user.ID, req.LastReadMsgID); err != nil {
		_ = c.Error(err)
		return
	}
	r.NoContent(c)
}

func (h *MessageHandler) EditMessage(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	msgID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.BadRequest("Invalid message ID"))
		return
	}
	var req dto.EditMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	msg, err := h.ms.EditMessage(c, user.ID, msgID, req.Content)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.OK(c, msg, "Message edited successfully")
}

func (h *MessageHandler) DeleteMessage(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	msgID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.BadRequest("Invalid message ID"))
		return
	}
	if err := h.ms.DeleteMessage(c, user.ID, msgID); err != nil {
		_ = c.Error(err)
		return
	}
	r.NoContent(c)
}

func (h *MessageHandler) ToggleReaction(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	msgID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.BadRequest("Invalid message ID"))
		return
	}
	var req dto.TogglePinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	action, err := h.ms.ToggleReaction(c, user.ID, msgID, req.Emoji)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.OK(c, &gin.H{"action": action}, "Toggle reaction successfully")
}

func (h *MessageHandler) toAttachmentDomain(attachReq []dto.AttachmentRequest) []*model.Attachment {
	attachments := make([]*model.Attachment, len(attachReq))
	for i, req := range attachReq {
		attachments[i] = &model.Attachment{
			FileURL:       req.FileURL,
			Filename:      req.FileName,
			MimeType:      req.MimeType,
			FileSizeBytes: req.FileSizeBytes,
			Width:         req.Width,
			Height:        req.Height,
			DurationSec:   req.DurationSec,
		}
	}
	return attachments
}
