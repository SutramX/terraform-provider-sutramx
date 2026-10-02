data "sutramx_plans" "all" {}

output "plan_prices_usd" {
  value = { for plan in data.sutramx_plans.all.plans : plan.id => plan.price_monthly_usd }
}
