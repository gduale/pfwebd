# Complete example: manage an OpenBSD PF firewall with Terraform
# through pfwebd. Run pfwebd in mock mode to try it locally:
#
#   PFWEBD_TOKEN=devtoken pfwebd -mock
#   PFWEBD_TOKEN=devtoken terraform plan

terraform {
  required_providers {
    pfwebd = {
      source  = "gduale/pfwebd"
      version = "~> 0.1"
    }
  }
}

provider "pfwebd" {
  endpoint = "http://192.168.64.5:8080"
  # token read from PFWEBD_TOKEN environment variable
}

data "pfwebd_status" "fw" {}

# resource "pfwebd_table" "blocklist" {
#   name = "blocklist"
#   addresses = [
#     "203.0.113.66",
#     "198.51.100.0/24",
#   ]
# }

resource "pfwebd_anchor_ruleset" "main" {
  rules = [
    "pass in log (all) on vio0 inet proto icmp icmp-type echoreq",
    "pass in log on vio0 proto tcp from 192.168.64.0/24 to (vio0) port 443",
  ]

  # The blocklist must be populated before rules referencing it apply.
  #depends_on = [pfwebd_table.blocklist]
}

output "pf_enabled" {
  value = data.pfwebd_status.fw.enabled
}
