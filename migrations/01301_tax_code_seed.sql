-- Seeded tax codes (MST-097, D-MST-16): the seven Taxable rates plus
-- Nil-Rated, Exempted and Non-GST at 0. Rates are thousandths of a percent.
-- created_by NULL = seeded by migration.
-- +goose Up
INSERT INTO tax_code (code, name, treatment, rate, created_at, updated_at) VALUES
    ('GST0.25', 'GST 0.25%', 'Taxable',     250, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('GST3',    'GST 3%',    'Taxable',    3000, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('GST5',    'GST 5%',    'Taxable',    5000, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('GST12',   'GST 12%',   'Taxable',   12000, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('GST18',   'GST 18%',   'Taxable',   18000, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('GST28',   'GST 28%',   'Taxable',   28000, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('GST40',   'GST 40%',   'Taxable',   40000, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('NIL',     'Nil-Rated', 'Nil-Rated',     0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('EXEMPT',  'Exempted',  'Exempted',      0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('NONGST',  'Non-GST',   'Non-GST',       0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- +goose Down
DELETE FROM tax_code WHERE created_by IS NULL;
