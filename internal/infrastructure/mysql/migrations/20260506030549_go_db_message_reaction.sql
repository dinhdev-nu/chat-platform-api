-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `message_reactions` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `message_id` BINARY(16)      NOT NULL                       COMMENT 'FK → messages.id',
  `user_id`    BINARY(16)      NOT NULL                       COMMENT 'FK → users.id',
  `emoji`      VARCHAR(10)     NOT NULL                       COMMENT 'Unicode emoji — vd: 👍 😂 🔥',
  `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_reaction` (`message_id`, `user_id`, `emoji`),
  -- [FIX #7] idx_reactions_message đã bị xóa: prefix của uq_reaction cover đủ

  CONSTRAINT `fk_reactions_message`
    FOREIGN KEY (`message_id`) REFERENCES `messages` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_reactions_user`
    FOREIGN KEY (`user_id`)    REFERENCES `users`    (`id`) ON DELETE CASCADE

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Reaction emoji — Slack-style';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `message_reactions`;
-- +goose StatementEnd
