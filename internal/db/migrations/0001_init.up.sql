CREATE TABLE devices (
    device_id BIGINT(20) UNSIGNED NOT NULL AUTO_INCREMENT,
    key_id VARCHAR(255) NOT NULL,
    public_key BLOB NOT NULL,
    receipt BLOB NULL,
    trust_weight DECIMAL(5,2) NOT NULL DEFAULT 1.00,
    report_count INT(11) UNSIGNED NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (device_id),
    UNIQUE KEY uq_devices_key_id (key_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE phone_numbers (
    phone_number_id BIGINT(20) UNSIGNED NOT NULL AUTO_INCREMENT,
    number VARCHAR(20) NOT NULL,
    cached_score DECIMAL(10,4) NOT NULL DEFAULT 0.0000,
    status ENUM('unknown','suspected','blocked','allowlisted','overridden_block') NOT NULL DEFAULT 'unknown',
    top_category ENUM('scam','robocall','telemarketer','other') NULL,
    report_count INT(11) UNSIGNED NOT NULL DEFAULT 0,
    counter_count INT(11) UNSIGNED NOT NULL DEFAULT 0,
    source ENUM('community','ftc','fcc') NOT NULL DEFAULT 'community',
    first_reported_at TIMESTAMP NULL,
    last_reported_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (phone_number_id),
    UNIQUE KEY uq_phone_numbers_number (number),
    KEY idx_phone_numbers_status (status),
    KEY idx_phone_numbers_status_updated_at (status, updated_at),
    KEY idx_phone_numbers_number (number)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE reports (
    report_id BIGINT(20) UNSIGNED NOT NULL AUTO_INCREMENT,
    device_id BIGINT(20) UNSIGNED NOT NULL,
    phone_number_id BIGINT(20) UNSIGNED NOT NULL,
    category ENUM('scam','robocall','telemarketer','other') NOT NULL DEFAULT 'other',
    vote ENUM('spam','not_spam') NOT NULL DEFAULT 'spam',
    weight DECIMAL(6,3) NOT NULL DEFAULT 1.000,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (report_id),
    UNIQUE KEY uq_reports_device_id_phone_number_id (device_id, phone_number_id),
    KEY idx_reports_phone_number_id (phone_number_id),
    CONSTRAINT fk_reports_device_id FOREIGN KEY (device_id) REFERENCES devices (device_id),
    CONSTRAINT fk_reports_phone_number_id FOREIGN KEY (phone_number_id) REFERENCES phone_numbers (phone_number_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE caller_names (
    caller_name_id BIGINT(20) UNSIGNED NOT NULL AUTO_INCREMENT,
    phone_number_id BIGINT(20) UNSIGNED NOT NULL,
    device_id BIGINT(20) UNSIGNED NOT NULL,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (caller_name_id),
    UNIQUE KEY uq_caller_names_device_id_phone_number_id (device_id, phone_number_id),
    KEY idx_caller_names_phone_number_id (phone_number_id),
    CONSTRAINT fk_caller_names_phone_number_id FOREIGN KEY (phone_number_id) REFERENCES phone_numbers (phone_number_id),
    CONSTRAINT fk_caller_names_device_id FOREIGN KEY (device_id) REFERENCES devices (device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE admin_overrides (
    admin_override_id INT(11) UNSIGNED NOT NULL AUTO_INCREMENT,
    phone_number_id BIGINT(20) UNSIGNED NOT NULL,
    mode ENUM('allow','block') NOT NULL,
    reason VARCHAR(255) NULL,
    admin VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (admin_override_id),
    UNIQUE KEY uq_admin_overrides_phone_number_id (phone_number_id),
    CONSTRAINT fk_admin_overrides_phone_number_id FOREIGN KEY (phone_number_id) REFERENCES phone_numbers (phone_number_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
