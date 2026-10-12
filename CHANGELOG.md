# Changelog

All notable changes to this provider are documented here. Versions follow
[Semantic Versioning](https://semver.org/); releases are the `vX.Y.Z` git tags.

## 0.2.0 (October 13, 2026)

BREAKING CHANGES:

* `sutramx_alert_channel`: `name` is limited to 60 characters at plan time, the limit the API has always enforced (the provider allowed 80, and longer names failed at apply).
* `sutramx_status_page`: `slug`, `logo_url` and `monitors[].monitor_id` are checked at plan time with the API's rules: a slug with a leading, trailing or double hyphen or a reserved word (`admin`, `api`, `status`, ...), a logo URL with a user name or password or over 2048 characters, a `monitor_id` that is not a UUID, and a monitor listed twice in `monitors` are plan errors. The API refused these values (or, for a repeated monitor, kept only the first entry, which failed apply with an inconsistent result).

ENHANCEMENTS:

* Retries are now safe for creates and never repeat an action:
  * The `POST` creates the API runs once per `Idempotency-Key` (status pages and alert channels) send a key, the same one on each retry, and are retried on network errors, timeouts and HTTP 502/503/504. A `409 IDEMPOTENCY_REQUEST_IN_PROGRESS` answer (the first try is still running) waits `Retry-After` and asks again with the same key. Servers without Idempotency-Key support ignore the header.
  * The read-only `POST /automation/monitors/plan` is retried like a read.
  * Any other `POST` (an action such as acknowledge, resolve, run-check, pause, test or rotate-secret) carries no key and is sent once after a network error, a timeout or a 5xx, since a retry could run it twice.
  * A 429 is retried for every request, actions included: the API's rate limiter answers before the request runs.
  * `409 IDEMPOTENT_RESPONSE_WITHHELD` (the first try succeeded, but its answer held a secret shown only once) is returned as an error, not retried. Rotate the channel's secret if you did not save it.
* `Retry-After` is read as seconds or as an HTTP date and honoured up to 120 seconds per wait (was 30), never past the context deadline. A 429 is retried up to 10 times, so `terraform destroy` of many monitors waits out the API's rate limit.
* Plan-limit refusals (`category: PLAN_LIMIT`, or codes such as `ENTITLEMENT_LIMIT_REACHED`, `MONITOR_TOTAL_LIMIT_REACHED`, `CONNECTION_LIMIT_REACHED`, `FEATURE_NOT_AVAILABLE`) are never retried and fail with a "plan limit reached" diagnostic.
* Warnings the API attaches to a monitor, status page or alert channel write are shown as Terraform warnings.
* Requests send `X-SutramX-Client: terraform-provider-sutramx/<version>` next to the User-Agent.
* `sutramx_monitor`: the Arizona, USA monitoring location code is now `usa-az`. `regions` still accepts the earlier code and treats it as equal to `usa-az`, so a configuration that uses it plans no changes (the API stores and returns `usa-az`). Use `usa-az` in new configurations; with provider 0.1.2 or older, change the code to `usa-az` in `regions` to avoid an "inconsistent result after apply" error.

NOTES:

* Docs: set `key` on every `sutramx_monitor`. A generated key is only saved once apply records the monitor in state, so an apply interrupted after the create (a timeout, a cancelled run) made a second monitor on the next apply.
* Docs: the "Secrets in state" section no longer lists `api_key`. Terraform does not store provider configuration in state; plan files (`terraform plan -out`) do hold input variable values, so prefer `SUTRAMX_API_KEY`.
* Docs: `sutramx_alert_channel` `name` is no longer described as unique; the API does not enforce it.

## 0.1.2 (October 10, 2026)

No provider code changes since 0.1.0. There is no 0.1.1 release: that tag was never published.

NOTES:

* Docs: a new "Secrets in state" section explains that `Sensitive` attributes are stored in plain text in the state file, and how to protect state.
* Release pipeline: GoReleaser is pinned to v2.18.3, and the job that holds the signing key no longer uses a Go cache.

## 0.1.0 (October 6, 2026)

First public release.

FEATURES:

* **New resource:** `sutramx_monitor`: HTTP, API, ping, port, UDP, cron (heartbeat), DNS, multi-step and MCP server monitors, written through the idempotent `PUT /automation/monitors/{key}` endpoint. Import by id or key.
* **New resource:** `sutramx_status_page`: status page settings and the ordered list of monitors shown on it. Import by id.
* **New resource:** `sutramx_alert_channel`: integration connections (webhook, Slack, and other channel types) with `all`, `groups` or `monitors` alert routing. Needs an API key with automation access. Import by id.
* **New data source:** `sutramx_regions`: monitoring locations.
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
* `sutramx_monitor`: `regions` on a `cron` (heartbeat) monitor is a plan-time error, since heartbeat monitors are not checked from monitoring locations and the API refuses regions for them. The provider never sends `regions` for cron monitors and keeps them null in state.
