-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `message_status` (
  `message_id` BINARY(16)  NOT NULL                           COMMENT 'FK → messages.id',
  `user_id`    BINARY(16)  NOT NULL                           COMMENT 'FK → users.id',
  `read_at`    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
                                                              COMMENT '[FIX #2] Thời điểm đọc — record tồn tại = đã đọc, không có record = chưa đọc',

  PRIMARY KEY (`message_id`, `user_id`),
  KEY `idx_status_user` (`user_id`),

  CONSTRAINT `fk_status_message`
    FOREIGN KEY (`message_id`) REFERENCES `messages` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_status_user`
    FOREIGN KEY (`user_id`)    REFERENCES `users`    (`id`) ON DELETE CASCADE

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Trạng thái đọc — record tồn tại = đã đọc, không có record = chưa đọc';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `message_status`;
-- +goose StatementEnd
