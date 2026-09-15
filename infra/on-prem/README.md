# On-prem reference provider

This profile is a certification target, not a laptop demonstration topology.
`bootstrap-on-prem-reference.sh` installs version-pinned providers into the
existing three-node Cilium kind cluster, while `certify-on-prem-reference.sh`
emits a tier-bound evidence bundle. Site installations may replace kind and
local storage, but changing a component or version invalidates certification.

No credential is committed. The bootstrap generates ephemeral local secrets;
persistent installations must source them from their secret-management system.
The profile uses an internal CA and RFC2136 DNS boundaries, so no public domain
or SaaS provider is required. ADR 0023's installer materializes the exact
candidate workspace and its conformance fixture into the in-cluster, read-only
Git service; Argo CD and preview acceptance therefore remain site-local.

Use [`docs/guides/on-prem-usage-guide.md`](../../docs/guides/on-prem-usage-guide.md)
for the end-to-end build, mirror, render, install, acceptance, upgrade, rollback,
and recovery procedure.
