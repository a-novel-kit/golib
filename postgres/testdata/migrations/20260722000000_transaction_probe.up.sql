-- A table the transaction tests write to. It exists only so a rollback has
-- something observable to discard: the assertion is whether the row is there,
-- so the columns are the minimum that makes a row identifiable.
CREATE TABLE transaction_probe (
  id text PRIMARY KEY NOT NULL CHECK (id <> '')
);
