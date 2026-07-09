# Clone Strategy Taxonomy Specification

## Purpose

Define Dolly clone strategies as logical single-database strategies and physical cluster-level strategies so operators choose the correct path for database size and topology.

## Requirements

### Requirement: Two-family strategy taxonomy

The system MUST describe clone strategies using two families: logical single-database clone strategies (`template`, `schema-replay`, `logical-stream`) and physical cluster-level clone strategies (`physical-backup`).

#### Scenario: Logical family is presented

- GIVEN operator-facing clone strategy guidance is shown
- WHEN the available strategies are listed
- THEN `template`, `schema-replay`, and `logical-stream` are identified as logical single-database strategies
- AND `logical-stream` is recommended for large single-database cross-server clones

#### Scenario: Physical family is presented

- GIVEN operator-facing clone strategy guidance is shown
- WHEN `physical-backup` is listed
- THEN it is identified as a physical cluster-level copy using PostgreSQL base backup semantics
- AND it is not described as the default production-scale single-database path

### Requirement: Canonical names remain stable

The system SHALL keep canonical strategy names as `template`, `schema-replay`, `logical-stream`, and `physical-backup`; compatibility aliases MAY exist only where explicitly specified by strategy resolution requirements.

#### Scenario: Canonical strategy list

- GIVEN an operator asks which names should be configured
- WHEN strategy guidance is rendered
- THEN the canonical list is `template`, `schema-replay`, `logical-stream`, and `physical-backup`
