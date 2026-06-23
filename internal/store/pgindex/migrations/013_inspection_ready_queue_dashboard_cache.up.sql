DELETE FROM public.indexer_dashboard_stats
WHERE stat_key IN (
    'pending_inspect_par2_binaries',
    'pending_inspect_archive_binaries',
    'pending_inspect_media_binaries'
);
