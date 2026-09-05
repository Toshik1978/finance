CREATE TABLE IF NOT EXISTS currencies
(
    id         INTEGER PRIMARY KEY      NOT NULL,
    date       DATE                     NOT NULL,
    base_code  VARCHAR(16)              NOT NULL,
    code       VARCHAR(16)              NOT NULL,
    value      DECIMAL                  NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS currencies_index ON currencies (date, base_code, code);

CREATE TABLE IF NOT EXISTS currency_settings
(
    key        VARCHAR(256) PRIMARY KEY NOT NULL,
    value      VARCHAR(256)             NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
