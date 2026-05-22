-- +goose Up
-- +goose StatementBegin
ALTER TABLE `conversations`
  ADD COLUMN `last_message_text` VARCHAR(1000) DEFAULT NULL AFTER `last_message_id`;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE `conversations`
  DROP COLUMN `last_message_text`;
-- +goose StatementEnd
