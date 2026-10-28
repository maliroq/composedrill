CREATE TABLE IF NOT EXISTS recovery_probe (id integer PRIMARY KEY, marker text NOT NULL);
INSERT INTO recovery_probe (id, marker) VALUES (1, 'backup-restored') ON CONFLICT (id) DO UPDATE SET marker = EXCLUDED.marker;
