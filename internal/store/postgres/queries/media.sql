-- upload_parts ----------------------------------------------------------------

-- name: SaveUploadPart :exec
INSERT INTO upload_parts (owner_user_id, file_id, part, total_parts, is_big, backend, object_key, size, sha256)
VALUES (
  sqlc.arg(owner_user_id)::bigint,
  sqlc.arg(file_id)::bigint,
  sqlc.arg(part)::int,
  sqlc.arg(total_parts)::int,
  sqlc.arg(is_big)::boolean,
  sqlc.arg(backend)::text,
  sqlc.arg(object_key)::text,
  sqlc.arg(size)::bigint,
  sqlc.arg(sha256)::bytea
)
ON CONFLICT (owner_user_id, file_id, part) DO UPDATE SET
  total_parts = EXCLUDED.total_parts,
  is_big = EXCLUDED.is_big,
  backend = EXCLUDED.backend,
  object_key = EXCLUDED.object_key,
  size = EXCLUDED.size,
  sha256 = EXCLUDED.sha256,
  created_at = now();

-- name: GetUploadPartUsage :one
SELECT
  COALESCE(SUM(size), 0)::bigint AS bytes,
  COUNT(*)::int AS parts,
  COUNT(DISTINCT file_id)::int AS files
FROM upload_parts
WHERE owner_user_id = sqlc.arg(owner_user_id)::bigint;

-- name: GetUploadPartSlot :one
SELECT
  COALESCE((
    SELECT size
    FROM upload_parts
    WHERE owner_user_id = sqlc.arg(owner_user_id)::bigint
      AND file_id = sqlc.arg(file_id)::bigint
      AND part = sqlc.arg(part)::int
  ), -1)::bigint AS existing_bytes,
  COALESCE((
    SELECT object_key
    FROM upload_parts
    WHERE owner_user_id = sqlc.arg(owner_user_id)::bigint
      AND file_id = sqlc.arg(file_id)::bigint
      AND part = sqlc.arg(part)::int
  ), '')::text AS object_key,
  (
    SELECT COUNT(*)::int
    FROM upload_parts
    WHERE owner_user_id = sqlc.arg(owner_user_id)::bigint
      AND file_id = sqlc.arg(file_id)::bigint
  )::int AS file_parts;

-- name: ListUploadParts :many
SELECT part, total_parts, is_big, backend, object_key, size, sha256
FROM upload_parts
WHERE owner_user_id = sqlc.arg(owner_user_id)::bigint
  AND file_id = sqlc.arg(file_id)::bigint
ORDER BY part ASC;

-- name: DeleteUploadParts :many
DELETE FROM upload_parts
WHERE owner_user_id = sqlc.arg(owner_user_id)::bigint
  AND file_id = sqlc.arg(file_id)::bigint
RETURNING object_key;

-- name: DeleteExpiredUploadParts :many
WITH doomed AS (
  SELECT owner_user_id, file_id, part
  FROM upload_parts
  WHERE created_at < sqlc.arg(before)::timestamptz
  ORDER BY created_at ASC
  LIMIT sqlc.arg(batch_limit)::int
)
DELETE FROM upload_parts p
USING doomed d
WHERE p.owner_user_id = d.owner_user_id
  AND p.file_id = d.file_id
  AND p.part = d.part
RETURNING p.object_key;

-- file_blobs ------------------------------------------------------------------

-- name: PutFileBlob :exec
INSERT INTO file_blobs (location_key, backend, object_key, size, sha256, mime_type)
VALUES (
  sqlc.arg(location_key)::text,
  sqlc.arg(backend)::text,
  sqlc.arg(object_key)::text,
  sqlc.arg(size)::bigint,
  sqlc.arg(sha256)::bytea,
  sqlc.arg(mime_type)::text
)
ON CONFLICT (location_key) DO UPDATE SET
  backend = EXCLUDED.backend,
  object_key = EXCLUDED.object_key,
  size = EXCLUDED.size,
  sha256 = EXCLUDED.sha256,
  mime_type = EXCLUDED.mime_type;

-- name: GetFileBlob :one
SELECT location_key, backend, object_key, size, sha256, mime_type
FROM file_blobs
WHERE location_key = sqlc.arg(location_key)::text;

-- documents -------------------------------------------------------------------

-- name: PutDocument :exec
INSERT INTO documents (id, access_hash, file_reference, date, mime_type, size, dc_id, attributes, thumbs)
VALUES (
  sqlc.arg(id)::bigint,
  sqlc.arg(access_hash)::bigint,
  sqlc.arg(file_reference)::bytea,
  sqlc.arg(date)::int,
  sqlc.arg(mime_type)::text,
  sqlc.arg(size)::bigint,
  sqlc.arg(dc_id)::int,
  sqlc.arg(attributes_json)::jsonb,
  sqlc.arg(thumbs_json)::jsonb
)
ON CONFLICT (id) DO UPDATE SET
  access_hash = EXCLUDED.access_hash,
  file_reference = EXCLUDED.file_reference,
  date = EXCLUDED.date,
  mime_type = EXCLUDED.mime_type,
  size = EXCLUDED.size,
  dc_id = EXCLUDED.dc_id,
  attributes = EXCLUDED.attributes,
  thumbs = EXCLUDED.thumbs;

-- name: GetDocument :one
SELECT id, access_hash, file_reference, date, mime_type, size, dc_id,
  attributes::text AS attributes_json,
  thumbs::text AS thumbs_json
FROM documents
WHERE id = sqlc.arg(id)::bigint;

-- name: GetDocuments :many
SELECT id, access_hash, file_reference, date, mime_type, size, dc_id,
  attributes::text AS attributes_json,
  thumbs::text AS thumbs_json
FROM documents
WHERE id = ANY(sqlc.arg(ids)::bigint[]);

-- photos ----------------------------------------------------------------------

-- name: PutPhoto :exec
INSERT INTO photos (id, access_hash, file_reference, date, dc_id, has_stickers, sizes)
VALUES (
  sqlc.arg(id)::bigint,
  sqlc.arg(access_hash)::bigint,
  sqlc.arg(file_reference)::bytea,
  sqlc.arg(date)::int,
  sqlc.arg(dc_id)::int,
  sqlc.arg(has_stickers)::boolean,
  sqlc.arg(sizes_json)::jsonb
)
ON CONFLICT (id) DO UPDATE SET
  access_hash = EXCLUDED.access_hash,
  file_reference = EXCLUDED.file_reference,
  date = EXCLUDED.date,
  dc_id = EXCLUDED.dc_id,
  has_stickers = EXCLUDED.has_stickers,
  sizes = EXCLUDED.sizes;

-- name: GetPhoto :one
SELECT id, access_hash, file_reference, date, dc_id, has_stickers,
  sizes::text AS sizes_json
FROM photos
WHERE id = sqlc.arg(id)::bigint;

-- sticker_sets ----------------------------------------------------------------

-- name: PutStickerSet :exec
INSERT INTO sticker_sets (
  id, access_hash, short_name, title, count, hash, set_kind,
  official, animated, videos, emojis, masks, text_color, installed, archived, installed_date,
  thumb_document_id, thumbs, thumb_dc_id, thumb_version, document_ids, packs, sort_order, system_key
) VALUES (
  sqlc.arg(id)::bigint,
  sqlc.arg(access_hash)::bigint,
  sqlc.arg(short_name)::text,
  sqlc.arg(title)::text,
  sqlc.arg(count)::int,
  sqlc.arg(hash)::int,
  sqlc.arg(set_kind)::text,
  sqlc.arg(official)::boolean,
  sqlc.arg(animated)::boolean,
  sqlc.arg(videos)::boolean,
  sqlc.arg(emojis)::boolean,
  sqlc.arg(masks)::boolean,
  sqlc.arg(text_color)::boolean,
  sqlc.arg(installed)::boolean,
  sqlc.arg(archived)::boolean,
  sqlc.arg(installed_date)::int,
  sqlc.arg(thumb_document_id)::bigint,
  sqlc.arg(thumbs_json)::jsonb,
  sqlc.arg(thumb_dc_id)::int,
  sqlc.arg(thumb_version)::int,
  sqlc.arg(document_ids_json)::jsonb,
  sqlc.arg(packs_json)::jsonb,
  sqlc.arg(sort_order)::int,
  sqlc.arg(system_key)::text
)
ON CONFLICT (id) DO UPDATE SET
  access_hash = EXCLUDED.access_hash,
  short_name = EXCLUDED.short_name,
  title = EXCLUDED.title,
  count = EXCLUDED.count,
  hash = EXCLUDED.hash,
  set_kind = EXCLUDED.set_kind,
  official = EXCLUDED.official,
  animated = EXCLUDED.animated,
  videos = EXCLUDED.videos,
  emojis = EXCLUDED.emojis,
  masks = EXCLUDED.masks,
  text_color = EXCLUDED.text_color,
  installed = EXCLUDED.installed,
  archived = EXCLUDED.archived,
  installed_date = EXCLUDED.installed_date,
  thumb_document_id = EXCLUDED.thumb_document_id,
  thumbs = EXCLUDED.thumbs,
  thumb_dc_id = EXCLUDED.thumb_dc_id,
  thumb_version = EXCLUDED.thumb_version,
  document_ids = EXCLUDED.document_ids,
  packs = EXCLUDED.packs,
  sort_order = EXCLUDED.sort_order,
  system_key = EXCLUDED.system_key;

-- name: GetStickerSetByID :one
SELECT
  id, access_hash, short_name, title, count, hash, set_kind,
  official, animated, videos, emojis, masks, installed, archived, installed_date,
  thumb_document_id, thumbs::text AS thumbs_json, thumb_dc_id, thumb_version,
  document_ids::text AS document_ids_json, packs::text AS packs_json, sort_order, system_key
FROM sticker_sets
WHERE id = sqlc.arg(id)::bigint;

-- name: GetStickerSetByShortName :one
SELECT
  id, access_hash, short_name, title, count, hash, set_kind,
  official, animated, videos, emojis, masks, installed, archived, installed_date,
  thumb_document_id, thumbs::text AS thumbs_json, thumb_dc_id, thumb_version,
  document_ids::text AS document_ids_json, packs::text AS packs_json, sort_order, system_key
FROM sticker_sets
WHERE short_name = sqlc.arg(short_name)::text;

-- name: GetStickerSetBySystemKey :one
SELECT
  id, access_hash, short_name, title, count, hash, set_kind,
  official, animated, videos, emojis, masks, installed, archived, installed_date,
  thumb_document_id, thumbs::text AS thumbs_json, thumb_dc_id, thumb_version,
  document_ids::text AS document_ids_json, packs::text AS packs_json, sort_order, system_key
FROM sticker_sets
WHERE system_key = sqlc.arg(system_key)::text;

-- name: ListStickerSetsByKind :many
SELECT
  id, access_hash, short_name, title, count, hash, set_kind,
  official, animated, videos, emojis, masks, installed, archived, installed_date,
  thumb_document_id, thumbs::text AS thumbs_json, thumb_dc_id, thumb_version,
  document_ids::text AS document_ids_json, packs::text AS packs_json, sort_order, system_key
FROM sticker_sets
WHERE set_kind = sqlc.arg(set_kind)::text
ORDER BY sort_order ASC, id ASC;

-- name: CountStickerSets :one
SELECT count(*)::int AS total FROM sticker_sets;

-- available_reactions ---------------------------------------------------------

-- name: PutAvailableReaction :exec
INSERT INTO available_reactions (
  reaction, title, inactive, premium,
  static_icon_id, appear_animation_id, select_animation_id,
  activate_animation_id, effect_animation_id, around_animation_id, center_icon_id, sort_order
) VALUES (
  sqlc.arg(reaction)::text,
  sqlc.arg(title)::text,
  sqlc.arg(inactive)::boolean,
  sqlc.arg(premium)::boolean,
  sqlc.arg(static_icon_id)::bigint,
  sqlc.arg(appear_animation_id)::bigint,
  sqlc.arg(select_animation_id)::bigint,
  sqlc.arg(activate_animation_id)::bigint,
  sqlc.arg(effect_animation_id)::bigint,
  sqlc.arg(around_animation_id)::bigint,
  sqlc.arg(center_icon_id)::bigint,
  sqlc.arg(sort_order)::int
)
ON CONFLICT (reaction) DO UPDATE SET
  title = EXCLUDED.title,
  inactive = EXCLUDED.inactive,
  premium = EXCLUDED.premium,
  static_icon_id = EXCLUDED.static_icon_id,
  appear_animation_id = EXCLUDED.appear_animation_id,
  select_animation_id = EXCLUDED.select_animation_id,
  activate_animation_id = EXCLUDED.activate_animation_id,
  effect_animation_id = EXCLUDED.effect_animation_id,
  around_animation_id = EXCLUDED.around_animation_id,
  center_icon_id = EXCLUDED.center_icon_id,
  sort_order = EXCLUDED.sort_order;

-- name: ListAvailableReactions :many
SELECT
  reaction, title, inactive, premium,
  static_icon_id, appear_animation_id, select_animation_id,
  activate_animation_id, effect_animation_id, around_animation_id, center_icon_id, sort_order
FROM available_reactions
ORDER BY sort_order ASC, reaction ASC;

-- name: CountAvailableReactions :one
SELECT count(*)::int AS total FROM available_reactions;

-- profile_photos --------------------------------------------------------------

-- name: AddProfilePhoto :exec
INSERT INTO profile_photos (owner_peer_type, owner_peer_id, photo_id, date, active, sort_order)
VALUES (
  sqlc.arg(owner_peer_type)::text,
  sqlc.arg(owner_peer_id)::bigint,
  sqlc.arg(photo_id)::bigint,
  sqlc.arg(date)::int,
  true,
  sqlc.arg(sort_order)::bigint
)
ON CONFLICT (owner_peer_type, owner_peer_id, photo_id) DO UPDATE SET
  date = EXCLUDED.date,
  active = true,
  sort_order = EXCLUDED.sort_order;

-- name: NextProfilePhotoOrder :one
SELECT COALESCE(MAX(sort_order), 0)::bigint AS max_order
FROM profile_photos
WHERE owner_peer_type = sqlc.arg(owner_peer_type)::text
  AND owner_peer_id = sqlc.arg(owner_peer_id)::bigint;

-- name: CurrentProfilePhoto :one
SELECT photo_id
FROM profile_photos
WHERE owner_peer_type = sqlc.arg(owner_peer_type)::text
  AND owner_peer_id = sqlc.arg(owner_peer_id)::bigint
  AND active
ORDER BY sort_order DESC
LIMIT 1;

-- name: CurrentProfilePhotosForOwners :many
SELECT DISTINCT ON (pp.owner_peer_id)
  pp.owner_peer_id,
  pp.photo_id,
  ph.dc_id,
  ph.sizes::text AS sizes_json
FROM profile_photos pp
JOIN photos ph ON ph.id = pp.photo_id
WHERE pp.owner_peer_type = sqlc.arg(owner_peer_type)::text
  AND pp.owner_peer_id = ANY(sqlc.arg(owner_ids)::bigint[])
  AND pp.active
ORDER BY pp.owner_peer_id, pp.sort_order DESC;

-- name: ListProfilePhotos :many
SELECT photo_id
FROM profile_photos
WHERE owner_peer_type = sqlc.arg(owner_peer_type)::text
  AND owner_peer_id = sqlc.arg(owner_peer_id)::bigint
  AND active
  AND (sqlc.arg(max_id)::bigint <= 0 OR photo_id < sqlc.arg(max_id)::bigint)
ORDER BY sort_order DESC
OFFSET sqlc.arg(offset_count)::int
LIMIT sqlc.arg(limit_count)::int;

-- name: CountProfilePhotos :one
SELECT count(*)::int AS total
FROM profile_photos
WHERE owner_peer_type = sqlc.arg(owner_peer_type)::text
  AND owner_peer_id = sqlc.arg(owner_peer_id)::bigint
  AND active;

-- name: DeactivateProfilePhotos :many
UPDATE profile_photos
SET active = false
WHERE owner_peer_type = sqlc.arg(owner_peer_type)::text
  AND owner_peer_id = sqlc.arg(owner_peer_id)::bigint
  AND photo_id = ANY(sqlc.arg(photo_ids)::bigint[])
  AND active
RETURNING photo_id;
