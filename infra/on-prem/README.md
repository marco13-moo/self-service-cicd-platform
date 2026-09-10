# On-prem reference provider

This profile is a certification target, not a laptop demonstration topology.
`bootstrap-on-prem-reference.sh` installs version-pinned providers into the
existing three-node Cilium kind cluster, while `certify-on-prem-reference.sh`
emits a tier-bound evidence bundle. Site installations may replace kind and
local storage, but changing a component or version invalidates certification.

No credential is committed. The bootstrap generates ephemeral local secrets;
persistent installations must source them from their secret-management system.
The profile uses an internal CA and RFC2136 DNS boundaries, so no public domain
or SaaS provider is required. The Argo drift test currently fetches the
upstream Argo CD guestbook fixture from GitHub; mirror that repository into the
site Git service when certifying an air-gapped installation.
