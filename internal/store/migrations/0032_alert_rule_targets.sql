-- Rules for one app or database instance go when it goes (target_id has no foreign
-- key: it names either table). Rules left behind by earlier deletes are removed.
DELETE FROM alert_rules WHERE target_id <> ''
 AND target_id NOT IN (SELECT id FROM apps) AND target_id NOT IN (SELECT id FROM db_instances);
CREATE TRIGGER alert_rules_app_deleted AFTER DELETE ON apps
BEGIN DELETE FROM alert_rules WHERE target_id = OLD.id; END;
CREATE TRIGGER alert_rules_db_instance_deleted AFTER DELETE ON db_instances
BEGIN DELETE FROM alert_rules WHERE target_id = OLD.id; END;
