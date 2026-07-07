DROP TABLE IF EXISTS public.binaries;
DROP SEQUENCE IF EXISTS public.binaries_id_seq;

DELETE FROM public.indexer_table_write_ownership
WHERE table_name = 'binaries';
