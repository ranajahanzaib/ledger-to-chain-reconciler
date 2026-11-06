-- Source schema supplied for application owners. ledgerchain never executes it.
-- Amounts MUST use TEXT; optional fields may be NULL or empty text.
CREATE TABLE ledger_entries (
    entry_id TEXT PRIMARY KEY NOT NULL,
    account_id TEXT NOT NULL,
    chain_id TEXT NOT NULL,
    token_address TEXT NOT NULL,
    deposit_address TEXT NOT NULL,
    expected_amount_base_units TEXT NOT NULL,
    tx_hash TEXT,
    log_index TEXT,
    credit_status TEXT NOT NULL CHECK (credit_status IN ('expected', 'credited')),
    credited_at_block TEXT,
    recorded_block_hash TEXT
);
