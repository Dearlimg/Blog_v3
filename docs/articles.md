# Personal Articles

The article module adds public reading at `/articles`, owner-only management at
`/studio/articles`, and a Markdown editor at `/studio/articles/:id/edit`.
Existing guestbook and comment features are unchanged; articles have no comment API.

## Owner Setup

Use an existing account's numeric ID as `blog_owner_user_id` in `config/config.yaml`,
or set `BLOG_OWNER_USER_ID`. The environment variable takes precedence. Zero or an
invalid environment value disables writing. The owner must still exist and must
authenticate using the existing login endpoint. Never select the owner from a
client-supplied email, role or request body. Registered readers cannot access drafts,
revisions, private assets, or mutation endpoints.

The first application start migrates the article tables through the existing GORM
AutoMigrate path. No application deployment is needed to build or test this module.

## Content and Publication

- Categories: fundamentals, project, notes, interview. Tags are a small JSON array
  stored with each draft/revision, so publication freezes tags along with content.
- `articles` identifies the owner, trash status, and current published revision.
- `article_drafts` contains the latest editable content and optimistic version.
- `article_revisions` holds immutable snapshots. Restore creates a new draft version
  and preserves the pre-restore draft. Auto snapshots retain the latest 100; manual
  and publication snapshots are retained without automatic deletion.
- `article_receipts` acknowledges idempotent mutations by article/request UUID and
  a payload fingerprint. Save receipts omit body copies. Receipts currently have no
  automatic expiry; include their metadata growth in database maintenance planning.
- `article_assets` stores PNG/JPEG/GIF images, limited to 5 MB, in MySQL. This avoids
  adding a separate unbacked filesystem volume. Images are public only while their
  article is published and its published body references that image URL.

All editable public fields are isolated from the published revision. Private notes
are never selected by the public API. Trash removes public access immediately;
restoring from trash leaves an unpublished draft. Publishing and version changes are
transactional. The service locks the article row, checks the expected version, then
updates the draft. Conflicts return 409; stale clients never overwrite silently.

The browser uses bundled CodeMirror, marked, DOMPurify, highlight.js and Lucide.
Runtime CDN availability is not required for the article UI. Raw HTML is sanitized;
external images are removed. Uploaded article images are the supported image path.

## Save and Recovery

Local IndexedDB writes debounce for 300 ms, with a maximum 1-second interval during
continuous typing. Server writes debounce for 2 seconds with a 10-second maximum.
Failed requests retain their exact request ID and payload for retries. New text
typed while a request is in flight stays dirty until its own acknowledgment.

The editor uses Web Locks to prevent two tabs in the same browser from editing one
article simultaneously. Other-device conflicts preserve both drafts and require an
explicit choice. Discarded local content remains an exportable backup in the studio.
JSON exports contain private notes and pending save state; Markdown exports contain
the title and body only. Imported backups replace content, never server version or
owner identity. Images remain server assets and are not embedded in these exports;
a database backup is required for a full restore to another server.

401/403 responses do not navigate away from the editor. Reauthentication resumes the
pending write. IndexedDB stores drafts under the validated owner ID and article ID.
The editor can reopen a previously saved local draft during an API outage if the
page assets load. It does not install a service worker: a completely offline cold
navigation may fail to load the page. Restoring connectivity permits normal recovery.
The browser profile stores private drafts even after logout; use a private device,
export needed drafts before clearing site data, and back up MySQL independently.

## Development and Verification

```sh
npm ci
npm run build:articles
go test ./...
go vet ./...
npx playwright install chromium --only-shell
npm run test:articles
```

Alternatively, set `PLAYWRIGHT_CHANNEL=msedge` to run against an installed Edge
browser without downloading Chromium.

Generated `static/article-build` assets are committed because the current production
Dockerfile copies static files without running a Node build. Run the build after
editing `view/JS/articles`. Go business tests and browser tests use isolated SQLite
databases. They do not connect to the configured production MySQL or Redis.
MySQL-specific schema/locking behavior should also be checked against a disposable
MySQL 8 instance before production rollout.

For an isolated local preview, set `ARTICLE_PREVIEW_PASSWORD` (at least 12 characters)
and run `go run ./scripts/article-preview`. Open `http://127.0.0.1:8086/articles` or
`http://127.0.0.1:8086/studio/articles`. Preview users are `owner@example.test` and
`reader@example.test`, both using that environment password. The preview binds only
to loopback and stores data in ignored `work/articles-preview.db`; it does not read
the application configuration or alter existing user accounts.
