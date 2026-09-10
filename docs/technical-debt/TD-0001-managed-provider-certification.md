# TD-0001: Managed-provider certification

- Status: Accepted debt
- Owner: Platform Engineering
- Introduced: 2026-09-09
- Review trigger: before offering a managed-cloud deployment profile
- Exit criterion: passing `managed-provider` evidence for every advertised provider

## Debt

ADR 0021 is initially certified against the on-prem reference profile. Cloud
portability follows provider-neutral interfaces and declarative substitutions,
but AWS, Azure, and Google Cloud integrations have not been operationally
certified. Therefore cloud RPO/RTO, workload identity, KMS, registry, DNS,
LoadBalancer, and managed PostgreSQL claims are explicitly out of scope.

## Risk and containment

Provider semantics can diverge around identity token audiences, key rotation,
OCI referrers, restore points, DNS convergence, and LoadBalancer failure modes.
Every evidence bundle carries `certification_tier`; admission and promotion reject
an unrecognized or weaker tier. Documentation may say “on-prem certified and
cloud-ready by design,” never “cloud certified.”

## Repayment plan

For each selected cloud, instantiate the existing provider contract, execute the
same release, identity, evidence, HA/DR, edge, and onboarding suites, retain the
signed evidence bundle, and add the exact provider/version tuple to the certified
matrix. No architectural waiver can substitute for execution.
