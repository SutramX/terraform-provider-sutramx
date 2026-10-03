resource "sutramx_monitor" "website" {
  key              = "web/home"
  name             = "Website"
  url              = "https://www.example.com"
  interval_seconds = 60
  regions          = ["fra1", "usa-az-probe"]
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

output "nightly_job_heartbeat_url" {
  value     = sutramx_monitor.nightly_job.heartbeat_url
  sensitive = true
}
