# Maintenance windows are created in the dashboard by the workspace owner;
# Terraform can read them.
data "sutramx_maintenance_windows" "ongoing" {
  effective_status = "ongoing"
}

output "in_maintenance" {
  value = length(data.sutramx_maintenance_windows.ongoing.windows) > 0
}
