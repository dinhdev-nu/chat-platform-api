-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `conversation_members` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `conversation_id` BINARY(16)      NOT NULL                  COMMENT 'FK → conversations.id',
  `user_id`         BINARY(16)      NOT NULL                  COMMENT 'FK → users.id',
  `role`            TINYINT         NOT NULL DEFAULT 3        COMMENT '1=owner 2=admin 3=member — TINYINT tránh ALTER lock',
  `is_muted`        TINYINT(1)      NOT NULL DEFAULT 0        COMMENT '1 = tắt thông báo',
  `last_read_at`    DATETIME        DEFAULT NULL              COMMENT 'Fallback watermark khi Redis miss hoặc message_status chưa backfill.
                                                               Cập nhật = MAX(ms.read_at) khi user đóng conversation.
                                                               KHÔNG dùng để đếm unread: dùng Redis unread:{uid}:{cid} hoặc message_status.',
  `joined_at`       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_conv_member`   (`conversation_id`, `user_id`),
  KEY `idx_members_user`        (`user_id`, `joined_at` DESC),
  KEY `idx_members_conv`        (`conversation_id`),

  CONSTRAINT `fk_members_conv`
    FOREIGN KEY (`conversation_id`) REFERENCES `conversations` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_members_user`
    FOREIGN KEY (`user_id`)         REFERENCES `users`         (`id`) ON DELETE CASCADE

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Thành viên trong hội thoại';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `conversation_members`;
-- +goose StatementEnd
