-- Short-lived, cross-replica daemon requests. No foreign keys: the requester
-- deletes its row on completion; expired rows are reclaimed on the next request.
CREATE TABLE IF NOT EXISTS autorouter_decision (
    id uuid PRIMARY KEY,
    runtime_id uuid NOT NULL,
    input jsonb NOT NULL,
    result jsonb,
    claimed boolean NOT NULL DEFAULT false,
    claim_until timestamptz,
    expires_at timestamptz NOT NULL
);
