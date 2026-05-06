-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS `user_contacts` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `user_id`    BINARY(16)   NOT NULL                          COMMENT 'Người gửi lời mời — FK → users.id',
  `contact_id` BINARY(16)   NOT NULL                          COMMENT 'Người nhận lời mời — FK → users.id',
  `status`     TINYINT      NOT NULL DEFAULT 1                COMMENT '1=pending 2=accepted 3=blocked',
  `created_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_contact_pair`    (`user_id`, `contact_id`),
  KEY        `idx_contact_id`     (`contact_id`),
  KEY        `idx_contact_status` (`user_id`, `status`),

  CONSTRAINT `fk_contact_user`
    FOREIGN KEY (`user_id`)    REFERENCES `users` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_contact_target`
    FOREIGN KEY (`contact_id`) REFERENCES `users` (`id`) ON DELETE CASCADE

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Danh bạ và trạng thái kết bạn';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `user_contacts`;
-- +goose StatementEnd
