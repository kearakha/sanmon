-- +goose Up
CREATE TABLE keys (
    id                        bigserial PRIMARY KEY,
    name                      text NOT NULL,
    token_hash                text NOT NULL UNIQUE,
    monthly_budget_micro_usd  bigint,
    rpm_limit                 integer,
    log_bodies                boolean NOT NULL DEFAULT false,
    disabled                  boolean NOT NULL DEFAULT false,
    created_at                timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE requests (
    id               bigserial PRIMARY KEY,
    created_at       timestamptz NOT NULL DEFAULT now(),
    key_id           bigint REFERENCES keys (id),
    model_requested  text NOT NULL,
    provider         text NOT NULL,
    model_resolved   text,
    stream           boolean NOT NULL DEFAULT false,
    status_code      integer,
    error            text,
    latency_ms       integer,
    ttfb_ms          integer,
    tokens_in        integer,
    tokens_out       integer,
    cost_micro_usd   bigint,
    cost_unknown     boolean NOT NULL DEFAULT false,
    partial          boolean NOT NULL DEFAULT false,
    request_body     jsonb,
    response_body    text
);

CREATE INDEX requests_created_at_idx ON requests (created_at DESC);
CREATE INDEX requests_key_id_idx ON requests (key_id);

-- +goose Down
DROP TABLE requests;
DROP TABLE keys;
