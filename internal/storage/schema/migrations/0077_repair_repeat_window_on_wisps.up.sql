-- Repair for stores that consumed the first cut of 0076, which added
-- repeat_window to issues only. Every wisp insert and every shared scan
-- broke fleet-wide until the wisps twin gained the column (gads-slack
-- found it live, 2026-10-28). Guarded, so on a fresh bundle where 0076
-- already covered both planes this is a no-op.

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
