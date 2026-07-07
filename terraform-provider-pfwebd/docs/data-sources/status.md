# Data source `pfwebd_status`

Reads the PF status from pfwebd. Useful to check connectivity to the firewall at plan time, gate resources, or expose the state as an output.

## Example

```hcl
data "pfwebd_status" "fw" {}

output "pf_enabled" {
  value = data.pfwebd_status.fw.enabled
}

output "pf_uptime" {
  value = data.pfwebd_status.fw.since
}

# Guard rail: fail if PF is disabled
resource "null_resource" "pf_must_be_enabled" {
  lifecycle {
    precondition {
      condition     = data.pfwebd_status.fw.enabled
      error_message = "PF is disabled on the target firewall."
    }
  }
}
```

## Schema reference

### Exported attributes

| Name | Type | Description |
|---|---|---|
| `id` | `string` | Always `pf`. |
| `enabled` | `bool` | `true` when PF is enabled. |
| `since` | `string` | How long PF has been running (e.g. `21 days 04:11:33`). |
