# Needs an API key created with "Automation access".
resource "sutramx_alert_channel" "ops_slack" {
  type = "slack"
  name = "Ops alerts"
  config = {
    webhook_url = var.slack_webhook_url
  }
  routing_scope = "monitors"
  monitor_ids   = [sutramx_monitor.website.id]
}

resource "sutramx_alert_channel" "incident_webhook" {
  type   = "webhook"
  name   = "Incident pipeline"
  config = { webhook_url = "https://hooks.example.com/sutramx" }
}

output "webhook_signing_secret" {
  value     = sutramx_alert_channel.incident_webhook.signing_secret
  sensitive = true
}

variable "slack_webhook_url" {
  type      = string
  sensitive = true
}
