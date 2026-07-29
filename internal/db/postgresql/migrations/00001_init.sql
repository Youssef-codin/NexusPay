-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TYPE transaction_status AS ENUM ('awaiting_payment', 'crediting', 'scheduled', 'completed', 'failed');
CREATE TYPE expense_category AS ENUM ('food', 'transport', 'bills', 'shopping', 'income', 'topup', 'other');

CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL UNIQUE,
    password            TEXT NOT NULL,
    full_name           TEXT NOT NULL,
    refresh_token       TEXT UNIQUE,
    token_expires_at    TIMESTAMPTZ,
    balance             BIGINT NOT NULL DEFAULT 0,
    is_system           BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,
    -- The clearing-account rule: only system users (Stripe) may go negative.
    -- Enforced row-level so no cross-table logic can be forgotten.
    CONSTRAINT balance_non_negative CHECK (balance >= 0 OR is_system)
);

CREATE TABLE transactions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_id           UUID NOT NULL REFERENCES users(id),
    receiver_id         UUID NOT NULL REFERENCES users(id),
    amount              BIGINT NOT NULL,
    status              transaction_status NOT NULL,
    note                TEXT,
    sender_category     expense_category,
    receiver_category   expense_category,
    scheduled_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT sender_ne_receiver CHECK (sender_id <> receiver_id),
    CONSTRAINT amount_positive    CHECK (amount > 0)
);

CREATE INDEX idx_users_name ON users(full_name);
CREATE INDEX idx_users_name_trgm ON users USING GIN (full_name gin_trgm_ops);

CREATE INDEX idx_transactions_sender_id ON transactions(sender_id);
CREATE INDEX idx_transactions_receiver_id ON transactions(receiver_id);

-- The scheduler's claim query.
CREATE INDEX idx_transactions_due ON transactions(scheduled_at) WHERE status = 'scheduled';
-- The sweeper's claim query: rows stuck mid-credit, found by updated_at.
CREATE INDEX idx_transactions_stuck ON transactions(updated_at) WHERE status = 'crediting';

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
NEW.updated_at = NOW();
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_set_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The sweeper's five-minute threshold reads updated_at, so entering
-- 'crediting' must stamp it.
CREATE TRIGGER transactions_set_updated_at
BEFORE UPDATE ON transactions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Stripe as a system user: a top-up is then an ordinary transaction and every
-- row nets to zero, which makes SUM(balance) = 0 a global invariant.
-- The empty password can never satisfy a bcrypt compare, so this row cannot log in.
INSERT INTO users (id, email, password, full_name, is_system)
VALUES ('00000000-0000-0000-0000-000000000001', 'stripe@system.nexuspay', '', 'Stripe', TRUE);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS users;

DROP FUNCTION IF EXISTS set_updated_at();

DROP TYPE IF EXISTS expense_category;
DROP TYPE IF EXISTS transaction_status;

DROP EXTENSION IF EXISTS pg_trgm;
-- +goose StatementEnd
