-- Widen call_type column to fit 'group_audio' / 'group_video' (was VARCHAR(10), too short)
ALTER TABLE call_logs ALTER COLUMN call_type TYPE VARCHAR(20);

-- Allow group call types in call_logs
ALTER TABLE call_logs DROP CONSTRAINT IF EXISTS call_logs_call_type_check;
ALTER TABLE call_logs ADD CONSTRAINT call_logs_call_type_check CHECK (call_type IN ('audio', 'video', 'group_audio', 'group_video'));

-- Also allow 'busy' status (used by P2P calls)
ALTER TABLE call_logs DROP CONSTRAINT IF EXISTS call_logs_status_check;
ALTER TABLE call_logs ADD CONSTRAINT call_logs_status_check CHECK (status IN ('ringing', 'answered', 'completed', 'missed', 'rejected', 'cancelled', 'busy'));
