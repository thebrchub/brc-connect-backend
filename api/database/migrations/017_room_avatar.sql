-- Add avatar_url to rooms for group avatars
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS avatar_url TEXT DEFAULT '';
