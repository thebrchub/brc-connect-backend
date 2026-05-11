-- 018_presence_hidden.sql
-- Allow users to hide their online presence from other users.
ALTER TABLE users ADD COLUMN IF NOT EXISTS presence_hidden BOOLEAN NOT NULL DEFAULT false;
