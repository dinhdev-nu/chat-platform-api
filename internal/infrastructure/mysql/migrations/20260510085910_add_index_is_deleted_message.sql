-- +goose Up
-- +goose StatementBegin
ALTER TABLE messages
  DROP   KEY   idx_msg_conv_cursor,
  ADD    KEY   idx_msg_conv_cursor (conversation_id, is_deleted, created_at DESC, seq DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE messages
  DROP   KEY   idx_msg_conv_cursor,
  ADD    KEY   idx_msg_conv_cursor (conversation_id, created_at DESC, seq DESC);
-- +goose StatementEnd