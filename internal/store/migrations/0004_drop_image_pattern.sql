-- Accepted since the first schema and never matched against anything: the
-- caller of a webhook is CI, which knows which image it pushed.
ALTER TABLE webhooks DROP COLUMN image_pattern;
