DELETE FROM public.indexer_dashboard_stats
WHERE stat_key IN (
    'pending_yenc_recovery_binaries',
    'pending_inspect_par2_binaries'
);
