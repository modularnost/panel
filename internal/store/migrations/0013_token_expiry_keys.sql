ALTER TABLE api_tokens ADD COLUMN expires_at_key TEXT;
CREATE INDEX api_tokens_expiry ON api_tokens (expires_at_key) WHERE expires_at_key IS NOT NULL;
