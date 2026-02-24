-- 011_pairing.sql
-- Phase 12: pairing codes for QR-based zero-config mobile pairing (CG11, R8).
-- A pairing code is a short-lived (15 min) UUID generated when the user requests
-- a QR code from the web UI. The mobile app scans the QR, sends the code to the
-- relay, which validates it and returns the NVR host URL + session token.
CREATE TABLE IF NOT EXISTS pairing_codes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT    NOT NULL UNIQUE,           -- UUID v4
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT    NOT NULL,                  -- RFC3339; 15-minute TTL
    used_at    TEXT,                              -- NULL until redeemed
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_pairing_codes_code ON pairing_codes(code);
CREATE INDEX IF NOT EXISTS idx_pairing_codes_expires_at ON pairing_codes(expires_at); -- for DELETE WHERE expires_at < ?
