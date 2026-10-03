-- +goose Up
CREATE TABLE IF NOT EXISTS user_permission_overrides (
    user_id    INTEGER NOT NULL,
    permission TEXT NOT NULL,
    granted    INTEGER NOT NULL CHECK (granted IN (0, 1)),
    PRIMARY KEY (user_id, permission),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS user_permission_overrides;
