# Resource `pfwebd_table`

**Authoritatively** manages the content (IP addresses and CIDR prefixes) of an existing PF table.

Authoritative means: on every apply, the provider adds the missing addresses **and removes the ones absent from the configuration**. An address added by hand (`pfctl -t blocklist -T add …`) will show up as drift and be removed on the next apply. This is the expected behaviour when Terraform is the source of truth.

## Firewall-side prerequisites

The resource does not create the table: it must exist in `pf.conf` and be allowed for writing in pfwebd.

```text
# /etc/pf.conf
table <blocklist> persist
block in quick on egress from <blocklist>
```

```sh
# pfwebd
pfwebd -rw-tables blocklist,allowlist ...
```

A write to a table not listed in `-rw-tables` fails with a 403 error.

## Example

```hcl
resource "pfwebd_table" "blocklist" {
  name = "blocklist"
  addresses = [
    "203.0.113.66",
    "198.51.100.0/24",
    "2001:db8:bad::/48",
  ]
}

# Tables combine nicely with for_each:
resource "pfwebd_table" "allowlist" {
  name      = "allowlist"
  addresses = [for ip in var.admin_ips : ip]
}
```

## Schema reference

### Arguments

| Name | Type | Required | Description |
|---|---|---|---|
| `name` | `string` | yes | PF table name. Must exist in `pf.conf` and be listed in `-rw-tables`. Changing the name **replaces** the resource (the old table is emptied). |
| `addresses` | `set(string)` | yes | Set of IP addresses (`203.0.113.66`) and/or CIDR prefixes (`198.51.100.0/24`), IPv4 and IPv6. |

### Exported attributes

| Name | Type | Description |
|---|---|---|
| `id` | `string` | Same as `name`. |

## Import

```sh
terraform import pfwebd_table.blocklist blocklist
```

## Deletion

`terraform destroy` **empties the table** (removes all its addresses) but does not remove it from `pf.conf` — that is impossible through the API, by design.
