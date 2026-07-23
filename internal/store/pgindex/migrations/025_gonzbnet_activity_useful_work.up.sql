ALTER TABLE federation_activity_rollups
  ADD COLUMN IF NOT EXISTS last_useful_at TIMESTAMPTZ;
