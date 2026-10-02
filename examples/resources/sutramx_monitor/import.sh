# By monitor id (UUID). A monitor made in the dashboard gets a key on the next apply.
terraform import sutramx_monitor.website 6f1c8f7e-6f2a-4a71-9d0b-0b8c1d2e3f40

# Or by key (a monitor created by sutramx.yml or Terraform).
terraform import sutramx_monitor.website web/home
