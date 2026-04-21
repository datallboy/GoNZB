ALTER TABLE scrape_checkpoints
ADD COLUMN IF NOT EXISTS backfill_article_number BIGINT NOT NULL DEFAULT 0;