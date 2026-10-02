terraform {
  required_providers {
    sutramx = {
      source  = "sutramx/sutramx"
      version = "~> 0.1"
    }
  }
}

# The API key can also come from SUTRAMX_API_KEY.
provider "sutramx" {
  api_key = var.sutramx_api_key
}

variable "sutramx_api_key" {
  type      = string
  sensitive = true
}
