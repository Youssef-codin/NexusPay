-- +goose Up
-- +goose StatementBegin
CREATE TYPE transaction_type AS ENUM ('debit', 'credit');
CREATE TYPE transaction_status AS ENUM ('pending', 'processing', 'completed', 'failed', 'reversed', 'reversing');
CREATE TYPE transfer_status AS ENUM ('pending', 'completed', 'failed');

CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL UNIQUE,
    password            TEXT NOT NULL,
    full_name           TEXT NOT NULL,
    refresh_token       TEXT UNIQUE,
    token_expires_at    TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);

CREATE TABLE wallets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL UNIQUE REFERENCES users(id),
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE TABLE transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    status          transfer_status NOT NULL DEFAULT 'pending',
    note            TEXT,
    debit_transaction_id    UUID UNIQUE,
    credit_transaction_id   UUID UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE transactions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id   UUID NOT NULL REFERENCES wallets(id),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    type        transaction_type NOT NULL,
    status      transaction_status NOT NULL DEFAULT 'pending',
    description TEXT DEFAULT NULL,
    transfer_id UUID REFERENCES transfers(id) DEFERRABLE INITIALLY DEFERRED,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

ALTER TABLE transfers
    ADD CONSTRAINT fk_debit_transaction
        FOREIGN KEY (debit_transaction_id) REFERENCES transactions(id) DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT fk_credit_transaction
        FOREIGN KEY (credit_transaction_id) REFERENCES transactions(id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE scheduled_transfers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id  UUID NOT NULL UNIQUE REFERENCES transfers(id),
    scheduled_at TIMESTAMPTZ NOT NULL,
    executed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX idx_users_name ON users(full_name);
CREATE INDEX idx_users_name_trgm ON users USING GIN (full_name gin_trgm_ops);
CREATE INDEX idx_wallets_user_id ON wallets(user_id);
CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);
CREATE INDEX idx_transactions_transfer_id ON transactions(transfer_id);
CREATE INDEX idx_transfers_from_wallet_id ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id ON transfers(to_wallet_id);
CREATE INDEX idx_scheduled_transfers_scheduled_at ON scheduled_transfers(scheduled_at);

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

CREATE TRIGGER wallets_set_updated_at
BEFORE UPDATE ON wallets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER transfers_set_updated_at
BEFORE UPDATE ON transfers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER scheduled_transfers_set_updated_at
BEFORE UPDATE ON scheduled_transfers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS scheduled_transfers_set_updated_at ON scheduled_transfers;
DROP TRIGGER IF EXISTS transfers_set_updated_at ON transfers;
DROP TRIGGER IF EXISTS wallets_set_updated_at ON wallets;
DROP TRIGGER IF EXISTS users_set_updated_at ON users;

DROP INDEX IF EXISTS idx_users_name_trgm;
DROP EXTENSION IF EXISTS pg_trgm;
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS scheduled_transfers;

ALTER TABLE transfers DROP CONSTRAINT IF EXISTS fk_debit_transaction;
ALTER TABLE transfers DROP CONSTRAINT IF EXISTS fk_credit_transaction;

DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS transfer_status;
DROP TYPE IF EXISTS transaction_status;
DROP TYPE IF EXISTS transaction_type;
-- +goose StatementEnd
