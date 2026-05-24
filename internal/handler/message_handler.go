package handler

import (
	"encoding/hex"
	"strconv"
	"time"

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
		if req.Content == "" {
			_ = c.Error(ae.New(ae.ErrValidation, "Message content cannot be empty"))
			return
		}

		msg, err := h.ms.SendMessage(c.Request.Context(), convID, user.ID, req.Type, req.Content, []byte(req.ParentID))
		if err != nil {
			_ = c.Error(err)
			return
		}
		out := h.msgToDTO(msg)
		r.OK(c, &out, "Send msg successfully")
		return
	}
	msgWithMeta, err := h.ms.SendMessageWithAttachment(c.Request.Context(), convID, user.ID, req.Type, req.Content, []byte(req.ParentID), h.toAttachmentDomain(req.Attachments))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := h.msgWithMetaToDTO(msgWithMeta)
	r.OK(c, &out, "Send msg with attachment successfully")
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
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid limit parameter"))
		return
	}
	msgs, err := h.ms.ListMessages(c.Request.Context(), user.ID, convID, &cursor, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	// map to DTOs
	items := make([]dto.MessageResponse, len(msgs.Items))
	for i, it := range msgs.Items {
		items[i] = h.msgWithMetaToDTO(it)
	}
	nextCursor := ""
	if msgs.NextCursor != nil {
		nextCursor = *msgs.NextCursor
	}
	r.Paginated(c, &items, &r.Pagination{
		Limit:      msgs.Limit,
		HasMore:    msgs.HasMore,
		NextCursor: nextCursor,
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
	if err := h.ms.MarkAsRead(c.Request.Context(), convID, user.ID, []byte(req.LastReadMsgID)); err != nil {
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
	msg, err := h.ms.EditMessage(c.Request.Context(), user.ID, msgID, req.Content)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := h.msgToDTO(msg)
	r.OK(c, &out, "Message edited successfully")
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
	if err := h.ms.DeleteMessage(c.Request.Context(), user.ID, msgID); err != nil {
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
	var req dto.ToggleReactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	action, err := h.ms.ToggleReaction(c.Request.Context(), user.ID, msgID, req.Emoji)
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

// --- mapping helpers (domain -> dto)
func (h *MessageHandler) msgToDTO(m *model.Message) dto.MessageResponse {
	var parentID, content, iv, deletedAt *string

	// ParentID: nil if empty, else pointer to hex string
	if len(m.ParentID) > 0 {
		pID := hex.EncodeToString(m.ParentID)
		parentID = &pID
	}

	// Content: nil or pointer to string
	if m.Content != nil {
		content = m.Content
	}

	// Iv: nil or pointer to string
	if m.Iv != nil {
		iv = m.Iv
	}

	// DeletedAt: nil if not deleted, else pointer to RFC3339 string
	if m.DeletedAt != nil {
		delStr := m.DeletedAt.Format(time.RFC3339)
		deletedAt = &delStr
	}

	return dto.MessageResponse{
		ID:               hex.EncodeToString(m.ID),
		ConversationID:   hex.EncodeToString(m.ConversationID),
		SenderID:         hex.EncodeToString(m.SenderID),
		ParentID:         parentID,
		Type:             int8(m.Type),
		Content:          content,
		ContentEncrypted: m.ContentEncrypted,
		Iv:               iv,
		Seq:              m.Seq,
		IsEdited:         m.IsEdited,
		IsDeleted:        m.IsDeleted,
		DeletedAt:        deletedAt,
		CreatedAt:        m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        m.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *MessageHandler) msgWithMetaToDTO(mm *model.MessageWithMeta) dto.MessageResponse {
	base := h.msgToDTO(mm.Message)
	// attachments
	atts := make([]dto.AttachmentResponse, 0, len(mm.Attachments))
	for _, a := range mm.Attachments {
		atts = append(atts, dto.AttachmentResponse{
			ID:            hex.EncodeToString(a.ID),
			MessageID:     hex.EncodeToString(a.MessageID),
			FileName:      a.Filename,
			FileURL:       a.FileURL,
			MimeType:      a.MimeType,
			FileSizeBytes: a.FileSizeBytes,
			Width:         a.Width,
			Height:        a.Height,
			DurationSec:   a.DurationSec,
			CreatedAt:     a.CreatedAt.Format(time.RFC3339),
		})
	}
	// reactions
	reacts := make([]dto.MessageReactionResponse, 0, len(mm.Reactions))
	for _, rct := range mm.Reactions {
		reacts = append(reacts, dto.MessageReactionResponse{
			ID:        rct.ID,
			MessageID: hex.EncodeToString(rct.MessageID),
			UserID:    hex.EncodeToString(rct.UserID),
			Emoji:     rct.Emoji,
			CreatedAt: rct.CreatedAt.Format(time.RFC3339),
		})
	}
	base.Attachments = atts
	base.Reactions = reacts
	base.SenderName = mm.SenderName
	base.SenderAvatarURL = mm.SenderAvatarURL
	return base
}
