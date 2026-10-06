# Changelog

All notable changes to this provider are documented here. Versions follow
[Semantic Versioning](https://semver.org/); releases are the `vX.Y.Z` git tags.

## 0.1.0 (Unreleased)

First public release.

FEATURES:

* **New resource:** `sutramx_monitor`: HTTP, API, ping, port, UDP, cron (heartbeat), DNS, multi-step and MCP server monitors, written through the idempotent `PUT /automation/monitors/{key}` endpoint. Import by id or key.
* **New resource:** `sutramx_status_page`: status page settings and the ordered list of monitors shown on it. Import by id.
* **New resource:** `sutramx_alert_channel`: integration connections (webhook, Slack, and other channel types) with `all`, `groups` or `monitors` alert routing. Needs an API key with automation access. Import by id.
* **New data source:** `sutramx_regions`: probe locations.
* **New data source:** `sutramx_plans`: plans, prices and limits.
* **New data source:** `sutramx_maintenance_windows`: maintenance windows (read-only).
* **New data source:** `sutramx_escalation_policies`: escalation policies (read-only).

NOTES:

* Stored monitor credentials (sensitive headers, tokens, URL passwords) and alert channel `config` are write-only in the API. The provider keeps the configured values in state and never reads them back.
* When the provider runs with a read-only API key, the API masks a cron monitor's `heartbeat_url`. The provider keeps the value it already has in state.
* Only `https://` API URLs are accepted. Requests are retried with backoff on 429, and on 502, 503 and 504 for idempotent methods.
* Values SutramX normalizes must be written in the stored form, or plan fails: monitor `name` and `url`, status page `title`, `description` and monitor `section` without leading or trailing whitespace; alert channel `name` also without repeated inner whitespace. Previously such values failed apply with "Provider produced inconsistent result after apply". An empty status page `description` or `section` is also rejected (leave the attribute out).
* `sutramx_monitor`: `url` is required at plan time for `http` and `api` monitors. For other types, removing `url` from the configuration now removes it in SutramX (before, the stored URL stayed and apply failed with an inconsistent result).
* `sutramx_monitor`: `url` and `config_json` are sensitive, because they can contain credentials (URL userinfo, tokens, headers, multi-step secrets). Plans do not show them, and an `output` that uses them needs `sensitive = true`.
* `sutramx_monitor`: `regions` on a `cron` (heartbeat) monitor is a plan-time error, since heartbeat monitors are not checked from probe locations and the API refuses regions for them. The provider never sends `regions` for cron monitors and keeps them null in state.
