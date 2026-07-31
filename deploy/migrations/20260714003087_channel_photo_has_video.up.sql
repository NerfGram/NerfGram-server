ALTER TABLE channels
    ADD COLUMN IF NOT EXISTS photo_has_video boolean DEFAULT false NOT NULL;

ALTER TABLE communities
    ADD COLUMN IF NOT EXISTS photo_has_video boolean DEFAULT false NOT NULL;

UPDATE channels c
SET photo_has_video = true
WHERE c.photo_id <> 0
  AND EXISTS (
    SELECT 1
    FROM photos p,
         jsonb_array_elements(p.sizes) s
    WHERE p.id = c.photo_id
      AND s->>'kind' IN ('video', 'video_emoji_markup', 'video_sticker_markup')
  );

UPDATE communities c
SET photo_has_video = true
WHERE c.photo_id <> 0
  AND EXISTS (
    SELECT 1
    FROM photos p,
         jsonb_array_elements(p.sizes) s
    WHERE p.id = c.photo_id
      AND s->>'kind' IN ('video', 'video_emoji_markup', 'video_sticker_markup')
  );
