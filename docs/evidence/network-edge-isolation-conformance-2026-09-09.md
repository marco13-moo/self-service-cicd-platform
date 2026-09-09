# Network and edge isolation conformance — 2026-09-09

## Environment

- kind: three-node `self-service-cicd-cilium` cluster
- Gateway API: v1.6.2 standard channel
- Cilium: 1.20.1, three ready agents, Gateway API enabled
- Workloads: disposable `alpha` and `beta` tenant namespaces plus a dedicated
  `preview-gateway` probe

## Verified controls

- Same-namespace service traffic succeeded.
- Alpha-to-beta and beta-to-alpha packets were denied.
- A probe in the dedicated preview gateway namespace reached the explicitly
  admitted alpha preview service.
- The Cilium Gateway reached `Accepted=True` and `Programmed=True`; an HTTP
  request carrying the governed alpha host traversed its generated LoadBalancer
  service and returned the alpha backend response.
- Admission accepted the alpha-owned governed host and rejected an alpha route
  attempting to claim a beta-prefixed host.
- DNS resolution succeeded for declared `example.com` and failed for an
  undeclared randomized exfiltration domain.
- Kubernetes API service and link-local cloud metadata access were denied.
- Deleting the generated Cilium allow policy did not open DNS egress; the
  namespace-level default-deny policy contracted connectivity as designed.
- The complete Go test suite, `go vet`, manifest server-side validation, and
  whitespace integrity checks passed for the implementation slice.

## Continuous enforcement

`network-isolation-conformance-cronjob.yaml` defines a six-hour, non-overlapping,
deadline-bounded execution. The harness refuses to begin unless every Cilium
agent is ready, so CNI degradation is reported as conformance failure rather
than producing a misleading isolation result.

The digest-pinned base conformance image was built locally, loaded into every
kind node, and instantiated from the CronJob as `network-conformance-manual`.
The Job reached `Complete` and emitted the same successful packet, edge, DNS,
metadata, control-plane, and policy-deletion assertions as the host-run suite.
