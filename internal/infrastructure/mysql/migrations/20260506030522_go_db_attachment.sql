-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `attachments` (
  `id`              BINARY(16)         NOT NULL               COMMENT 'UUID v7 — BINARY(16)',
  `message_id`      BINARY(16)         NOT NULL               COMMENT 'FK → messages.id',
  `file_name`       VARCHAR(255)       NOT NULL,
  `file_url`        VARCHAR(512)       NOT NULL               COMMENT 'S3/CDN URL',
  `mime_type`       VARCHAR(100)       NOT NULL,
  `file_size_bytes` BIGINT UNSIGNED    NOT NULL DEFAULT 0     COMMENT '[FIX #5] Kích thước byte — BIGINT tránh overflow file > 4GB',
  `width`           MEDIUMINT UNSIGNED DEFAULT NULL           COMMENT '[FIX #6] Pixels ảnh/video — MEDIUMINT max ~16.7M px',
  `height`          MEDIUMINT UNSIGNED DEFAULT NULL           COMMENT '[FIX #6] Pixels ảnh/video — MEDIUMINT max ~16.7M px',
  `duration_sec`    SMALLINT UNSIGNED  DEFAULT NULL           COMMENT 'Giây — audio hoặc video (SMALLINT max 65,535s ≈ 18h)',
  `created_at`      DATETIME           NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (`id`),
  KEY `idx_attachments_message` (`message_id`),

  CONSTRAINT `fk_attachments_message`
    FOREIGN KEY (`message_id`) REFERENCES `messages` (`id`) ON DELETE CASCADE

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='File đính kèm trong tin nhắn';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `attachments`;
-- +goose StatementEnd
