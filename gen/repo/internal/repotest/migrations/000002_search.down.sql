DROP TABLE boards;
DROP TABLE item_events;
DROP TABLE item_licenses;
DROP TABLE label_uses;
DROP TABLE labels;
ALTER TABLE audit_events DROP CONSTRAINT audit_events_item_id_fkey;
