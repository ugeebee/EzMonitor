BEGIN;
DROP TABLE IF EXISTS alert_rule_webhooks, webhooks, alert_logs, alert_rules,
    metrics_data, instance_labels, instances, user_group_roles, groups, users, roles CASCADE;
DROP FUNCTION IF EXISTS br7_freeze_resolved_alerts(), t1_reject_disabled_instances(),
    run_maintenance(int), drop_old_metrics_partitions(int), create_metrics_partition(date);
DROP TYPE IF EXISTS webhook_status, alert_status, cmp_op, metric_kind, instance_status;
COMMIT;
