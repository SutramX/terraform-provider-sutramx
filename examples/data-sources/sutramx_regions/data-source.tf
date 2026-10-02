data "sutramx_regions" "online" {
  online_only = true
}

output "region_codes" {
  value = data.sutramx_regions.online.codes
}
