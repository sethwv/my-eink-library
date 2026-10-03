-- +goose Up
-- Legacy can_bookmark values are copied into permission overrides by
-- migrateColumns after it ensures the additive legacy columns exist.

-- +goose Down
SELECT 1;
