# Terraform provider for SutramX

Manage [SutramX](https://sutramx.com) uptime monitoring with Terraform, built on the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).

| | |
|---|---|
| Resources | `sutramx_monitor`, `sutramx_status_page`, `sutramx_alert_channel` |
| Data sources | `sutramx_regions`, `sutramx_plans` |
| Import | monitors by id or key, status pages and alert channels by id |

```hcl
terraform {
  required_providers {
    sutramx = { source = "sutramx/sutramx" }
  }
}

provider "sutramx" {} # reads SUTRAMX_API_KEY

resource "sutramx_monitor" "website" {
  key     = "web/home"
  name    = "Website"
  url     = "https://www.example.com"
  regions = ["bom", "sin"]
}

resource "sutramx_status_page" "public" {
  title    = "Example status"
  slug     = "example-status"
  monitors = [{ monitor_id = sutramx_monitor.website.id }]
}
```

Full reference: [`docs/`](docs/index.md). Examples: [`examples/`](examples/).

## Authentication

Create an API key in SutramX under **Settings → API keys** and set `SUTRAMX_API_KEY` (or `api_key` in the provider block). The Free plan has no API keys. `sutramx_alert_channel` needs a key created with **Automation access**; monitors and status pages work with a standard key.

## How it maps to SutramX

- **Monitors** are written with the idempotent `PUT /automation/monitors/{key}` endpoint, the same one `sutramx.yml` uses, so a retried create never makes a duplicate. Without `key`, the provider generates one (`tf-...`). Optional fields you leave out (`config_json`, `tags`, `interval_seconds`) are left as they are on the server; `regions` left out means the plan's default locations. Changing `type` replaces the monitor. Importing a dashboard monitor gives it a key on the next apply.
- **Status pages** are matched by id; `monitors` is the ordered list shown on the page (omit it to manage the list in the dashboard).
- **Alert channels** are integration connections. Their `config` is stored encrypted and never read back, so Terraform cannot detect secret changes made in the dashboard.
- Plan limits are enforced by the API exactly as in the dashboard; errors such as `ENTITLEMENT_LIMIT_REACHED` come back as Terraform diagnostics.

## Development

Requires Go 1.25.8 or newer.

```bash
make build      # ./terraform-provider-sutramx
make test       # unit tests, including plan/apply/import/destroy against an in-memory fake API
make testacc    # acceptance tests: TF_ACC=1 against a real, disposable workspace
make docs       # regenerate docs/ from the schema and examples/
```

`make test` and `make testacc` need a Terraform CLI; terraform-plugin-testing downloads one when `TF_ACC_TERRAFORM_PATH` is not set.

Acceptance test environment:

| Variable | |
|---|---|
| `SUTRAMX_API_KEY` | key for a disposable workspace (tests create and delete resources) |
| `SUTRAMX_API_URL` | optional, e.g. a staging API |
| `SUTRAMX_ACC_ALERT_CHANNELS=1` and `SUTRAMX_ACC_WEBHOOK_URL` | also test `sutramx_alert_channel` (automation key required) |

### Local use before the registry release

```bash
make install
```

or add a `dev_overrides` block for `sutramx/sutramx` pointing at this directory in `~/.terraformrc`.

### Releasing

Tag `vX.Y.Z` and run goreleaser (`.goreleaser.yml`) with a GPG key registered in the Terraform Registry for the `sutramx` namespace.
