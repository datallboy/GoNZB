WITH ranked AS (
    SELECT
        rf.id,
        ROW_NUMBER() OVER (
            PARTITION BY rf.binary_id
            ORDER BY r.updated_at DESC, rf.updated_at DESC, rf.id DESC
        ) AS rn
    FROM release_files rf
    JOIN releases r ON r.release_id = rf.release_id
    WHERE rf.binary_id IS NOT NULL
),
doomed AS (
    SELECT id
    FROM ranked
    WHERE rn > 1
)
DELETE FROM release_file_articles
WHERE release_file_id IN (SELECT id FROM doomed);

WITH ranked AS (
    SELECT
        rf.id,
        ROW_NUMBER() OVER (
            PARTITION BY rf.binary_id
            ORDER BY r.updated_at DESC, rf.updated_at DESC, rf.id DESC
        ) AS rn
    FROM release_files rf
    JOIN releases r ON r.release_id = rf.release_id
    WHERE rf.binary_id IS NOT NULL
)
DELETE FROM release_files
WHERE id IN (
    SELECT id
    FROM ranked
    WHERE rn > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_release_files_binary_unique
    ON release_files (binary_id)
    WHERE binary_id IS NOT NULL;
