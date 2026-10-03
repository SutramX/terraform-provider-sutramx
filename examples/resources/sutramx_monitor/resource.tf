resource "sutramx_monitor" "website" {
  key              = "web/home"
  name             = "Website"
  url              = "https://www.example.com"
  interval_seconds = 60
  regions          = ["fra1", "bom"] # codes from the sutramx_regions data source
  tags             = ["prod", "web"]
  config_json = jsonencode({
    timeout               = 10000
    expected_status_codes = [200]
    keyword               = "Example Domain"
  })
}

resource "sutramx_monitor" "postgres" {
  key  = "infra/postgres"
  name = "Postgres"
  type = "port"
  config_json = jsonencode({
    host = "db.example.com"
    port = 5432
  })
}

resource "sutramx_monitor" "nightly_job" {
  key  = "jobs/nightly"
  name = "Nightly job"
  type = "cron"
  config_json = jsonencode({
    cron_expression = "0 2 * * *"
  })
}

# DNS records: alert when the MX records differ from the expected ones.
resource "sutramx_monitor" "mail_dns" {
  key  = "dns/mx"
  name = "Mail DNS"
  type = "dns"
  config_json = jsonencode({
    hostname        = "example.com"
    record_type     = "MX"
    dns_mode        = "expected"
    expected_values = ["10 mail.example.com"]
  })
}

# Multi-step API check: log in, then call an authenticated endpoint with the
# extracted token. secrets are write-only (sealed by SutramX, never read back).
resource "sutramx_monitor" "checkout" {
  key  = "api/checkout"
  name = "Checkout API"
  type = "multistep"
  config_json = jsonencode({
    steps = [
      {
        name    = "Log in"
        method  = "POST"
        url     = "https://api.example.com/login"
        headers = { "Content-Type" = "application/json" }
        body    = jsonencode({ api_key = "{{secrets.API_KEY}}" })
        extract = [{ name = "token", source = "json", expression = "$.token" }]
      },
      {
        name                  = "Cart"
        url                   = "https://api.example.com/cart"
        headers               = { Authorization = "Bearer {{token}}" }
        expected_status_codes = [200]
      },
    ]
    secrets = { API_KEY = var.checkout_api_key }
  })
}

variable "checkout_api_key" {
  type      = string
  sensitive = true
}

output "nightly_job_heartbeat_url" {
  value     = sutramx_monitor.nightly_job.heartbeat_url
  sensitive = true
}
