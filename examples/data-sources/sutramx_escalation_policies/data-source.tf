# Escalation policies are created in the dashboard by the workspace owner;
# Terraform can look them up by name.
data "sutramx_escalation_policies" "primary" {
  name = "Primary on-call"
}

output "primary_policy_id" {
  value = one(data.sutramx_escalation_policies.primary.policies[*].id)
}
