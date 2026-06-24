CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_blank_message_retire
ON public.yenc_recovery_work_items (updated_at, binary_id)
WHERE status IN ('ready', 'running')
  AND BTRIM(COALESCE(message_id, '')) = '';
