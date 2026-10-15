---
sidebar_position: 50
title: Routing sites through a VPN or another interface
description: Send the traffic of selected sites through a VPN tunnel or a second uplink of the router, and everything else by the normal route.
---

A set in **Network interface** mode sends the traffic of its sites through an interface of the router, such as a WireGuard or OpenVPN tunnel or a second uplink. Everything else keeps the normal route.

## Requirements

- An interface that is up on the router: a VPN client set up outside b4, such as WireGuard or OpenVPN, or a second uplink.
- The `ip` command and a firewall backend: nftables, or iptables with the `ipset` command.
- A set with the sites as domains, or with their IP addresses, CIDR ranges, GeoIP categories or ASNs. See [Adding sites to a set](./add-sites.md).

## Setup

1. Open **Sets** and open the set, or click **Create Set** and add the sites on the **Targets** tab.
2. Open the **DNS & Routing** tab, then **Traffic Routing**.
3. Turn on **Enable Routing**.
4. Set **Routing mode** to **Network interface**.
5. In **Output Interface**, select the VPN interface, such as `wg0`.
6. Turn on **Kill switch** if the set's sites must never use the normal uplink.
7. Click **Save Changes**, or **Create Set** for a new set.

The fields are described under [Network interface](../sets/routing.md#output-interface).

To send everything a device sends through the VPN, select the device under **Targets > Source Devices** and turn on **Match any IP address** under **Targets > Bypass IPs**.

## Checking the result

1. Add `ipinfo.io` to the set's domains for the test.
2. On a device, open `https://ipinfo.io/ip` or run `curl -s https://ipinfo.io/ip`. The address shown is the VPN's exit address, not the router's.

A single address like this is the clearer test: a full web page loads parts from other hosts, and it can look broken while routing works.

With a tunnel as the output interface, the set's connections end with `[routed->wg0]` on the **Logs** page. **System Info** on **Settings > System > Service** shows the number of installed routing sets in the **Routing Sets** row under **Firewall**.

## Kill switch and the router's own traffic

Without the **Kill switch**, the set's traffic leaves through the normal uplink with the router's real address while the VPN is down. With it, that traffic is dropped until the interface returns.

**Router's own traffic** decides whether connections the router opens itself follow the set. **Automatic** is the usual choice. A setup with Xray, sing-box or another proxy reading a TUN device is covered in [Using b4 with Xray or XrayUI](./xray.md).

## When it does not work

- **The set routes nothing.** Routing matches destination addresses, and the tab warns when the set has no domain, IP or ASN target. Source devices alone select no traffic.
- **The output interface is marked unavailable.** The VPN is down, or its interface has another name now. Routing resumes when the interface is back.
- **A device uses encrypted DNS.** b4 does not see that device's DNS answers, and the first connection to a new address can take the normal route. Turning off secure DNS in the browser, or Private DNS on Android, fixes it. See [DNS](../dns.md#what-b4-intercepts).
- **Another service routes the traffic first.** VPN and proxy tools add their own `ip rule` entries, and a rule with a lower priority takes the packets before b4's. See [Using b4 with Xray or XrayUI](./xray.md).
- **The Routing Sets row shows an error.** The firewall rejected the rules. [When the rules cannot be installed](../internals/routing.md#install-failures) lists the causes.
