-- Write your migrate up statements here

ALTER TABLE public.github_connections RENAME TO git_connections;

ALTER INDEX public.idx_github_connections_installation_id RENAME TO idx_git_connections_installation_id;
ALTER INDEX public.idx_github_connections_repo_full_name RENAME TO idx_git_connections_repo_full_name;

---- create above / drop below ----

ALTER TABLE public.git_connections RENAME TO github_connections;

ALTER INDEX public.idx_git_connections_installation_id RENAME TO idx_github_connections_installation_id;
ALTER INDEX public.idx_git_connections_repo_full_name RENAME TO idx_github_connections_repo_full_name;

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.