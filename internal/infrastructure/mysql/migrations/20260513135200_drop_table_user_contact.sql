-- +goose Up
-- +goose StatementBegin
DROP TABLE IF EXISTS `user_contacts`;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `user_contacts`;
-- +goose StatementEnd
