# YiMao storage boundary

This release does not perform a wholesale migration. Durable business records remain
owned by their existing services and files/databases; the in-memory session manager is
only an ephemeral UI/search cache and is never the authority for requests, approvals,
receipts, quotas, mappings, preferences, feedback, or bindings.

Authoritative durable stores:

- `review_requests.json`: request, approval/rejection, subscription link, receipt
  coordinates, retry/stuck markers and lifecycle state, owned by `ReviewService`.
- `user_mappings.db`: Telegram/MoviePilot mappings and notification preferences,
  owned by `UserMappingDB`; the legacy JSON is migration input only.
- `user_quotas.json`: quota state, owned by `QuotaService`.
- `preferences.json`: user preferences, owned by `PreferencesService`.
- `feedback.json`: issue/feedback state, owned by `IssueService`.
- `binding_requests.json`: binding workflow state, owned by `BindingRequestService`.
- `carpool.json`, `media_notifications.json`, `weekly_reports.json`, and the
  fulfillment/season JSON files: their corresponding service owns each file.
- `search_history.db`, `wishpool.db`, and `social.db`: SQLite-owned history,
  wish-pool, and social data respectively.

Migration boundary: future work should migrate the remaining JSON authorities behind
service interfaces one domain at a time, with dual-read verification and restart
fixtures before changing the on-disk authority. Existing files are not deleted or
silently rewritten by this release.
