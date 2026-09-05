-- The long-lived user token Meta issues lasts about 60 days. Storing when it
-- expires is what lets the server renew it before a tenant silently drops off.
-- 0 means unknown, which is the case for every tenant created before this
-- migration; they are refreshed on the next sweep and gain a real deadline.
ALTER TABLE tenants ADD COLUMN user_token_expires_at INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_tenants_token_expiry ON tenants(user_token_expires_at);
