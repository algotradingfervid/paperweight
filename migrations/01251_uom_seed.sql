-- Seeded UoMs with GST UQC codes (MST-078; HRS has no hours UQC and maps to
-- OTH, DRUM is DRM: CD-39, M9). created_by NULL = seeded by migration.
-- +goose Up
INSERT INTO uom (code, name, uqc, whole_number, created_at, updated_at) VALUES
    ('NOS',  'Numbers',       'NOS', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('KGS',  'Kilograms',     'KGS', 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('MTR',  'Meters',        'MTR', 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('LTR',  'Litres',        'LTR', 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('BOX',  'Box',           'BOX', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('SET',  'Sets',          'SET', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('BAG',  'Bags',          'BAG', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('TON',  'Tonnes',        'TON', 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('SQM',  'Square meters', 'SQM', 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('HRS',  'Hours',         'OTH', 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ('DRUM', 'Drums',         'DRM', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- +goose Down
DELETE FROM uom WHERE created_by IS NULL;
