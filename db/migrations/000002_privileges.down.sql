BEGIN;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM ezm_ingest, ezm_api, ezm_readonly;
REVOKE USAGE ON SCHEMA public FROM ezm_ingest, ezm_api, ezm_readonly;
GRANT EXECUTE ON FUNCTION create_metrics_partition(date),
                          drop_old_metrics_partitions(int),
                          run_maintenance(int) TO PUBLIC;
COMMIT;
