PRAGMA secure_delete=ON;
ALTER TABLE webhooks RENAME COLUMN secret_token TO token_hash;
UPDATE webhooks SET token_hash = panel_hash_token(token_hash);
