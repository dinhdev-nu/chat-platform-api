CREATE TABLE IF NOT EXISTS `users` (
  `id`           BINARY(16)   NOT NULL                        COMMENT 'UUID v7 dạng BINARY(16) — encode: UNHEX(REPLACE(uuid,\'-\',\'\'))',
  `username`     VARCHAR(50)  NOT NULL                        COMMENT 'Tên hiển thị — UNIQUE để tìm bạn',
  `email`        VARCHAR(255) NOT NULL                        COMMENT 'Email đăng nhập qua OTP',
  `avatar_url`   VARCHAR(512) DEFAULT NULL                    COMMENT 'S3/CDN URL ảnh đại diện',
  `bio`          VARCHAR(300) DEFAULT NULL                    COMMENT 'Giới thiệu bản thân',
  `status`       TINYINT      NOT NULL DEFAULT 1              COMMENT '1=active 2=suspended 3=deactivated — TINYINT thay ENUM tránh ALTER TABLE lock',
  `last_seen_at` DATETIME     DEFAULT NULL                    COMMENT 'Cập nhật khi WebSocket disconnect',
  `created_at`   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at`   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_users_username` (`username`),
  UNIQUE KEY `uq_users_email`    (`email`),
  KEY        `idx_users_status`  (`status`)

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Tài khoản người dùng — xác thực Email OTP';
