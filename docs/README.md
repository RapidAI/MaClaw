# Documentation Index

This directory contains architecture notes, design records, and operational guides.

## Agent Dynamic UI & Enterprise MIS Replacement

- [Agent dynamic UI runtime design](agent-dynamic-ui-runtime-design-zh.md): AG-UI event protocol, Skill/Tool/Business Object non-invasive adapters, right-side Task Panel, structured input validation, and business data persistence.
- [Agent dynamic UI implementation status](agent-dynamic-ui-runtime-implementation-status-zh.md): current MVP wiring for right-side operable UI, MIS, skill, and registered tool adapters.
- [Enterprise structured data design](maclaw-enterprise-structured-data-design-zh.md): MaClawDataSrv product and API design — datasets, business actions, views, dashboards, governance, and approval workflows.

## Knowledge Base (外脑)

- [Memory architecture improvement plan](memory-architecture-improvement-plan.md): Full architecture of the three-layer memory system (conversation history → long-term memory → cold storage) and improvement phases.

## MaClawDataSrv

- [MaClawDataSrv package boundary](datasrv-structureddata-boundary.md): current split between `corelib/structureddata`, `datasrv/structureddata`, and `datasrv/cmd/maclaw-data-srv`.
- [MaClawDataSrv production operations guide](datasrv-production-ops-guide.md): deployment, environment variables, backup verification, restore checklist, and offline administrator recovery.
