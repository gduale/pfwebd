# pfwebd Provider

The `pfwebd` provider manages an **OpenBSD PF** firewall through the REST API of [pfwebd](../../README.md), the daemon installed on the firewall.

It drives two things, and only those two:

- the **ruleset of the `pfwebd` anchor** (resource `pfwebd_anchor_ruleset`) — never `/etc/pf.conf`, which stays under your control;
- the **content of the PF tables** allowed for writing (resource `pfwebd_table`).

## Built-in anti-lockout

Every ruleset change follows the pfwebd cycle: `pfctl -n` validation → apply → **confirmation**. The provider confirms automatically after applying. If your new rules cut network access to the firewall, the confirmation request never gets through and pfwebd **rolls the change back on its own** (60 s by default). A `terraform apply` therefore cannot lock you out for good.

## Example usage

```hcl
terraform {
  required_providers {
    pfwebd = {
      source  = "gduale/pfwebd"
      version = "~> 0.1"
    }
  }
}

provider "pfwebd" {
  endpoint = "https://fw.example.org:8080"
  token    = var.pfwebd_token
}

variable "pfwebd_token" {
  type      = string
  sensitive = true
}

resource "pfwebd_anchor_ruleset" "main" {
  rules = [
    "block in quick on em0 from <blocklist>",
    "pass in on em0 proto tcp from 192.168.1.0/24 to (em0) port 443",
    "pass out on em0 inet keep state",
  ]
}

resource "pfwebd_table" "blocklist" {
  name = "blocklist"
  addresses = [
    "203.0.113.66",
    "198.51.100.0/24",
  ]
}
```

## Configuration of Terraform on your laptop

Because the Terraform provider isn't published yet, 
you should do this configuration in the file `~/.terraformrc`:

```
provider_installation {
  dev_overrides {
    "gduale/pfwebd" = "/Users/gduale/go/bin"
  }
```

## Provider reference

### Arguments

| Name | Type | Required | Description |
|---|---|---|---|
| `endpoint` | `string` | no | Base URL of the pfwebd API. Defaults to the `PFWEBD_ENDPOINT` environment variable, then `http://127.0.0.1:8080`. |
| `token` | `string`, sensitive | no | API token sent as `Authorization: Bearer`. Required for all writes (pfwebd is always read-only without it). Defaults to the `PFWEBD_TOKEN` environment variable. |

### Resources and data sources

| Name | Description |
|---|---|
| [`pfwebd_anchor_ruleset`](resources/anchor_ruleset.md) | Complete, ordered ruleset of the `pfwebd` anchor (singleton). |
| [`pfwebd_table`](resources/table.md) | Authoritative content of a PF table. |
| [`pfwebd_status` (data)](data-sources/status.md) | PF status (enabled, uptime). |

~> **Network security**: the pfwebd API is not natively encrypted. Expose it only on an admin network, or behind `relayd`/`httpd` with TLS, and always use a token.
