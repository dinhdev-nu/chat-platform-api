-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `conversations` (
  `id`               BINARY(16)   NOT NULL                    COMMENT 'UUID v7 — BINARY(16)',
  `type`             TINYINT      NOT NULL DEFAULT 1          COMMENT '1=direct 2=group 3=channel — TINYINT tránh ALTER lock',
  `name`             VARCHAR(100) DEFAULT NULL                COMMENT 'Tên nhóm — NULL với DM',
  `avatar_url`       VARCHAR(512) DEFAULT NULL,
  `description`      VARCHAR(500) DEFAULT NULL,
  `created_by`       BINARY(16)   DEFAULT NULL                COMMENT 'FK → users.id — NULL khi user đã xóa tài khoản',
  `last_message_id`  BINARY(16)   DEFAULT NULL                COMMENT 'Denormalized — NO FK — cập nhật qua Kafka worker',
  `last_activity_at` DATETIME     DEFAULT NULL                COMMENT 'Sort danh sách conversation',
  `created_at`       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (`id`),
  KEY `idx_conv_creator`       (`created_by`),
  KEY `idx_conv_last_activity` (`last_activity_at` DESC),

  CONSTRAINT `fk_conv_creator`
    FOREIGN KEY (`created_by`) REFERENCES `users` (`id`)
    ON DELETE SET NULL                 

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Hội thoại — DM, nhóm hoặc channel';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `conversations`;
-- +goose StatementEnd
