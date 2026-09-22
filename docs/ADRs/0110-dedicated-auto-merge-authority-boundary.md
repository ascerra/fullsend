---
title: "110. Dedicated auto-merge stage with host-side authorization"
status: Accepted
relates_to:
  - autonomy-spectrum
topics:
  - auto-merge
  - policy
  - least-privilege
  - forge
---

# 110. Dedicated auto-merge stage with host-side authorization

Date: 2026-09-09

## Status

Accepted

## Context

Fullsend has a legacy `CODE_AUTO_MERGE` path in the Code post-script, while the
product direction calls for a dedicated, opt-in auto-merge agent
([agents#1132](https://github.com/fullsend-ai/agents/issues/1132)). Keeping both
would create two Fullsend-owned ways to authorize autonomous merging and make
policy, revocation, and auditing ambiguous. Related Fullsend policy and evidence
work is tracked in [fullsend#3016](https://github.com/fullsend-ai/fullsend/issues/3016)
and [fullsend#6892](https://github.com/fullsend-ai/fullsend/issues/6892).

Review can advise whether a change is acceptable, but merge authorization also
depends on current forge policy, required checks and reviews, human vetoes, and
the exact revision GitHub may merge. An agent response cannot authoritatively
establish those mutable facts. The existing sandbox boundary already keeps
write credentials in trusted host code
([ADR 0017](0017-credential-isolation-for-sandboxed-agents.md)).

The first dogfood repositories, `fullsend-ai/fullsend` and
`fullsend-ai/agents`, require merge queues. The architecture therefore must
preserve the repository-selected direct or queue path rather than bypassing
queue policy or treating generic native auto-merge as the Fullsend mechanism.

## Options

- Keep auto-merge in Code or Review. Rejected because it combines assessment
  and irreversible authority and preserves multiple enablement paths.
- Give the model a merge-capable credential. Rejected because untrusted pull
  request content or compromised model output could invoke the mutation.
- Use a dedicated stage with a trusted host-side authorization gate. Chosen to
  separate semantic assessment from authoritative state and mutation.

## Decision

Fullsend will have exactly one Fullsend-owned autonomous-merge path: a dedicated
`auto-merge` stage. It is opt-in per repository and disabled by default. The
legacy Code post-script path and its `CODE_AUTO_MERGE*` settings will be retired
as the dedicated stage is implemented
([agents#1219](https://github.com/fullsend-ai/agents/pull/1219)); they will not
remain as a compatibility path.

The model is advisory. It may return a structured eligibility assessment, but
it receives no merge-capable credential and cannot authorize or invoke a forge
mutation. Trusted host code re-fetches authoritative state, applies current
policy and human vetoes, rejects unknown or stale state, and controls the final
transition through a dedicated least-privilege identity. Administrator bypass
is prohibited.

Authorization is bound to immutable repository, pull-request, revision, base,
and policy identities. The host must revalidate those bindings immediately
before any direct merge or queue enrollment and record the authorization and
outcome durably. A required merge queue remains required: enrollment must bind
the reviewed pull-request head, and the exact queue-generated merge-group
revision must receive fresh authorization before it can merge.

Detailed schemas, trigger rules, queue protocols, storage, reconciliation,
rollout gates, and cohort definitions are follow-up implementation design. They
must preserve this authority boundary and land in reviewable increments before
live mutation is enabled.

## Consequences

- Review and Auto-Merge can evolve independently without coupling model
  judgment to merge authority.
- A prompt-injected or compromised model cannot directly merge because the
  credential and final gate remain outside its sandbox.
- Direct and merge-queue repositories use their normal forge enforcement;
  Fullsend does not weaken or bypass repository requirements.
- Removing the legacy path requires explicit migration to dedicated-stage
  policy and avoids split-brain enablement.
- Implementation needs separate security, conformance, and rollout work before
  any repository enables autonomous mutation.
