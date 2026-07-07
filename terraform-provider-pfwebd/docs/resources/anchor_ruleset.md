# Resource `pfwebd_anchor_ruleset`

Manages the **complete, ordered** ruleset of the `pfwebd` PF anchor. This is a **singleton** resource: declare at most one instance per pfwebd server — the anchor has a single content, two instances would fight each other.

The order of the `rules` elements is the PF evaluation order (with the usual "last matching rule wins" semantics, except for `quick`).

## Apply cycle (anti-lockout)

On every create/update:

1. the ruleset is validated on the firewall (`pfctl -a pfwebd -n -f -`) — an invalid ruleset fails the apply without touching anything;
2. it is loaded into the anchor;
3. the provider sends the confirmation. If it never arrives (access cut by the new rules), pfwebd restores the previous ruleset after its delay (`-confirm-timeout`, 60 s by default).

If the confirmation fails, the provider also attempts an immediate cancellation so no pending change is left behind.

## Example

```hcl
resource "pfwebd_anchor_ruleset" "main" {
  rules = [
    # Drop the blocklist as early as possible
    "block in quick on em0 from <blocklist>",

    # Exposed services
    "pass in on em0 proto tcp from 192.168.1.0/24 to (em0) port { 22 443 }",

    # Outbound traffic
    "pass out on em0 inet keep state",
  ]
}
```

## Schema reference

### Arguments

| Name | Type | Required | Description |
|---|---|---|---|
| `rules` | `list(string)` | yes | Ordered list of PF rules, **one rule per element**. Only the `pass`, `block` and `match` actions are accepted; the `anchor`, `include` and `load` keywords are rejected. Rules are stored trimmed — do not include comments or empty lines, or you will get permanent drift. |

### Exported attributes

| Name | Type | Description |
|---|---|---|
| `id` | `string` | Always `pfwebd` (the managed anchor name). |

## Import

```sh
terraform import pfwebd_anchor_ruleset.main pfwebd
```

The anchor's active ruleset is then read from the firewall and compared against the configuration on the next plan (drift detection).

## Deletion

`terraform destroy` empties the anchor (`pfctl -a pfwebd -F rules`), still through the confirmation cycle. The rules in `/etc/pf.conf` are never touched.
