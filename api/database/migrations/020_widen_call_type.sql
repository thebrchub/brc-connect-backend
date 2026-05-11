-- Widen call_type column to fit 'group_audio' / 'group_video' (was VARCHAR(10), 11 chars needed)
ALTER TABLE call_logs ALTER COLUMN call_type TYPE VARCHAR(20);
