resource "sutramx_status_page" "public" {
  title               = "Example status"
  slug                = "example-status"
  description         = "Live status of Example's website and API."
  is_public           = true
  show_response_times = true
  accent_color        = "#0d9488"

  monitors = [
    { monitor_id = sutramx_monitor.website.id },
    { monitor_id = sutramx_monitor.postgres.id, section = "Infrastructure" },
  ]
}
