ALTER TABLE job_notifications
    ADD COLUMN message_id VARCHAR(32) NULL,
    ADD COLUMN notification_json JSON NULL,
    ADD COLUMN availability ENUM('open', 'closed', 'removed') NOT NULL DEFAULT 'open',
    ADD COLUMN availability_checked_at TIMESTAMP NULL,
    ADD KEY ix_job_notifications_availability (availability, availability_checked_at);
