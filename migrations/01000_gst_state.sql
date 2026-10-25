-- GST states: STATE_NUMBERS, india_compliance/gst_india/constants/__init__.py:96, verbatim.
-- Kept equal to gstin.States() (MST-039).
-- +goose Up
CREATE TABLE gst_state (
    code TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);
INSERT INTO gst_state (code, name) VALUES
    ('35', 'Andaman and Nicobar Islands'),
    ('37', 'Andhra Pradesh'),
    ('12', 'Arunachal Pradesh'),
    ('18', 'Assam'),
    ('10', 'Bihar'),
    ('04', 'Chandigarh'),
    ('22', 'Chhattisgarh'),
    ('26', 'Dadra and Nagar Haveli and Daman and Diu'),
    ('07', 'Delhi'),
    ('30', 'Goa'),
    ('24', 'Gujarat'),
    ('06', 'Haryana'),
    ('02', 'Himachal Pradesh'),
    ('01', 'Jammu and Kashmir'),
    ('20', 'Jharkhand'),
    ('29', 'Karnataka'),
    ('32', 'Kerala'),
    ('38', 'Ladakh'),
    ('31', 'Lakshadweep Islands'),
    ('23', 'Madhya Pradesh'),
    ('27', 'Maharashtra'),
    ('14', 'Manipur'),
    ('17', 'Meghalaya'),
    ('15', 'Mizoram'),
    ('13', 'Nagaland'),
    ('21', 'Odisha'),
    ('96', 'Other Countries'),
    ('97', 'Other Territory'),
    ('34', 'Puducherry'),
    ('03', 'Punjab'),
    ('08', 'Rajasthan'),
    ('11', 'Sikkim'),
    ('33', 'Tamil Nadu'),
    ('36', 'Telangana'),
    ('16', 'Tripura'),
    ('09', 'Uttar Pradesh'),
    ('05', 'Uttarakhand'),
    ('19', 'West Bengal');

-- +goose Down
DROP TABLE gst_state;
