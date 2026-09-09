-- Apply as the owner of audit_events after migration 5. The archive consumer
-- receives inserts only; mutation is independently prohibited by the source
-- table trigger. Slot lifecycle belongs to the managed PostgreSQL operator.
CREATE PUBLICATION platform_audit_events
  FOR TABLE audit_events
  WITH (publish = 'insert');
