-- +goose Up
ALTER TABLE book_enrichment ADD COLUMN chaptarr_status TEXT NOT NULL DEFAULT '';
ALTER TABLE book_enrichment ADD COLUMN hardcover_status TEXT NOT NULL DEFAULT '';

-- Backfill: historically status/source only ever recorded one combined
-- outcome, and "no_match" was only ever written by the Hardcover fallback
-- (see internal/web/enrichment.go's processHardcoverMatch), so this
-- backfill is exact, not a guess.
UPDATE book_enrichment SET hardcover_status = 'no_match' WHERE status = 'no_match';
UPDATE book_enrichment SET hardcover_status = 'done' WHERE status = 'done' AND source = 'hardcover';
UPDATE book_enrichment SET chaptarr_status = 'done' WHERE status = 'done' AND source = 'chaptarr';

-- +goose Down
-- SQLite's ALTER TABLE can't drop a column on the versions this app
-- targets; the columns are left in place (harmless, default '') on downgrade.
