-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `messages` (
  `id`                BINARY(16)      NOT NULL                COMMENT 'UUID v7 — BINARY(16), timestamp prefix đảm bảo insert tuần tự',
  `conversation_id`   BINARY(16)      NOT NULL                COMMENT 'FK → conversations.id',
  `sender_id`         BINARY(16)      NOT NULL                COMMENT 'FK → users.id',
  `parent_id`         BINARY(16)      DEFAULT NULL            COMMENT 'Reply/thread — self ref FK',
  `type`              TINYINT         NOT NULL DEFAULT 1      COMMENT '1=text 2=image 3=file 4=audio 5=video 6=system',
  `content`           MEDIUMTEXT      DEFAULT NULL            COMMENT 'Nội dung — MEDIUMTEXT ~16MB',
  `content_encrypted` TINYINT(1)      NOT NULL DEFAULT 0      COMMENT '1 = nội dung đã AES-256 encrypt',
  `iv`                CHAR(32)        DEFAULT NULL            COMMENT 'AES Initialization Vector — 16 bytes hex-encoded',
  `seq`               BIGINT UNSIGNED NOT NULL DEFAULT 0      COMMENT 'Thứ tự trong conversation — tăng từ Redis INCR seq:{conv_id}',
  `is_edited`         TINYINT(1)      NOT NULL DEFAULT 0,
  `is_deleted`        TINYINT(1)      NOT NULL DEFAULT 0      COMMENT 'Soft delete flag',
  `deleted_at`        DATETIME        DEFAULT NULL            COMMENT '[BONUS] Thời điểm soft delete — cleanup job xóa khi deleted_at < NOW()-30d',
  `created_at`        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3)    COMMENT 'Millisecond precision',
  `updated_at`        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
                                      ON UPDATE CURRENT_TIMESTAMP(3),

  PRIMARY KEY (`id`),
  KEY      `idx_msg_conv_cursor` (`conversation_id`, `created_at` DESC, `seq` DESC), -- cursor pagination
  KEY      `idx_msg_conv_seq`    (`conversation_id`, `seq`),                         -- ordering
  KEY      `idx_msg_sender`      (`sender_id`),
  KEY      `idx_msg_parent`      (`parent_id`),
  KEY      `idx_msg_cleanup`     (`deleted_at`),                                     -- [BONUS] cleanup job: WHERE deleted_at IS NOT NULL AND deleted_at < threshold
  FULLTEXT KEY `ft_msg_content`  (`content`)                                         -- tạm dùng, scale → Elasticsearch

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Tin nhắn — core table';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE `messages`
  ADD CONSTRAINT `fk_messages_conv`
    FOREIGN KEY (`conversation_id`) REFERENCES `conversations` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_messages_sender`
    FOREIGN KEY (`sender_id`)       REFERENCES `users`         (`id`),
  ADD CONSTRAINT `fk_messages_parent`
    FOREIGN KEY (`parent_id`)       REFERENCES `messages`      (`id`) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `messages`;
-- +goose StatementEnd
