# Architecture Documentation

> Mermaid-based architecture diagrams for the KubeAssist operator.

These documents describe the stable core architecture of the kube-assist-operator. They are intended for onboarding, code review, and design reference.

---

## Table of Contents

| # | Document | Description |
|---|----------|-------------|
| 0 | [System Overview](00-system-overview.md) | System context diagram, package dependency graph, binary composition |
| 1 | [Reconciliation Flows](01-reconciliation-flows.md) | Controller lifecycles (TeamHealthRequest, TroubleshootRequest, CheckPlugin), CR state machines |
| 2 | [Checker & Causal Engine](02-checker-registry.md) | Checker interface and registry, RunAll execution, causal correlation pipeline, cross-checker rules, AI batch enhancement |
| 3 | [AI, Dashboard & DataSource](03-ai-dashboard-datasource.md) | AI provider abstraction, optimization pipeline, dashboard HTTP routing, DataSource abstraction, scope resolver |

---

## Maintenance Policy

**Architecture-changing PRs must update `docs/architecture/`.**

When modifying controllers, adding checkers, changing the AI pipeline, or restructuring packages, update the relevant diagram(s) in this directory. Diagrams should reflect the code — not aspirational designs.

---

*Last reviewed: 2026-02-11*
