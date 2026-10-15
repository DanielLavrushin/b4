---
sidebar_position: 4
title: How TSPU works
description: TSPU, the Russian DPI system, and where its detailed technical description lives.
---

TSPU (Technical Means of Countering Threats, ТСПУ) is the Russian deep packet inspection (DPI) system installed in internet providers' networks. It is the system that blocks access to sites and services.

## Documentation

A detailed technical description of the TSPU (ASBI) architecture, its components, how it works and how it filters is kept in a separate project:

**[tspu-docs](https://github.com/DanielLavrushin/tspu-docs)** - a guide in 24 chapters, based on a lecture of about five hours on the architecture, configuration and operation of TSPU. The material is in Russian.

:::warning Accuracy
The documentation was generated with AI from the lecture transcript and checked by hand. It may be out of date: TSPU is updated continuously, and the current implementation can differ from the one described. Inaccuracies are possible.
:::

Main topics:

- The ASBI/TSPU architecture: balancers, filters, the DPI engine
- Blocking methods: DNS spoofing, SNI filtering, IP blocking
- Detection: how TSPU recognises bypass attempts
- Weak points: what b4's bypass strategies rely on

Knowing how TSPU works helps when choosing and tuning bypass strategies. It is not needed for everyday use of b4: [Discovery](../discovery.md) finds a working configuration automatically.
