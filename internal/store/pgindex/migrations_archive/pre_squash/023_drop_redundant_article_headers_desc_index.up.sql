-- The unique (newsgroup_id, article_number) constraint already supports the
-- hot header-lookup pattern; the separate DESC index is redundant.

DROP INDEX IF EXISTS idx_article_headers_newsgroup_id_article_number;
