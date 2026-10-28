-- Quiet window for a recurrence ("HH:MM-HH:MM IANA/Zone"). Outside it the
-- bead is deliberately silent: the due sweep does not fire it and the nag
-- checkers stay quiet. Empty means always live.
--
-- Both planes, like 0067's recurrence columns: the wisps twin shares the
-- issue row shape and is scanned by the same column list. The first cut of
-- this migration added the column to issues only and broke every wisp
-- insert and every shared scan fleet-wide; the ALTERs are guarded the same
-- way 0068's are so a replay onto a partly-migrated store is a no-op.

SET @needs_add = (
  SELECT COUNT(*) = 0
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND COLUMN_NAME = 'repeat_window'
);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE issues ADD COLUMN repeat_window TEXT',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_wisps = (
  SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps'
);
SET @needs_add = IF(@has_wisps = 0, 0, (
  SELECT COUNT(*) = 0
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'wisps'
      AND COLUMN_NAME = 'repeat_window'
));
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE wisps ADD COLUMN repeat_window TEXT',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
