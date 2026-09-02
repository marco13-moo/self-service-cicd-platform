# ADR 0012: OCI Preview Build, Deployment, and Routing

## Status

Accepted

## Context

The control plane can provision preview namespaces, reconcile exact source
revisions, observe Argo completion, and enforce TTL deletion. The deployment
Workflow currently validates checkout only; it does not produce an OCI image,
run the application, or publish a usable endpoint.

Embedding build commands or Kubernetes resource mutation in the Go control
plane would collapse the established intent/execution boundary. Coupling image
or route construction to GitHub or Bitbucket would likewise violate the
provider-neutral source-control model.

## Decision

### Repository build contract

Preview-enabled repositories MUST provide a Dockerfile at a service-configured
relative path, defaulting to `Dockerfile`. The platform does not infer an
application entry point from its detected language. Project type remains
classification metadata and may select richer policy in future revisions.

Argo executes a daemonless rootless BuildKit build. The builder:

- checks out the exact desired commit;
- builds from the repository Dockerfile;
- pushes to the configured OCI repository;
- tags the image with the complete commit SHA; and
- receives registry credentials only through an optional Kubernetes Secret
  mounted as Docker configuration.

No Docker socket or privileged container is exposed. BuildKit uses the upstream
rootless Kubernetes execution contract, including an unconfined seccomp and
AppArmor profile required for its user-namespace worker.

### Workload deployment contract

After a successful image push, the same Argo Workflow applies one Deployment
and one ClusterIP Service to the preview namespace. Resource names are stable
per environment; applying a new generation updates the image declaratively.
The Deployment carries service, environment, source-revision, and managed-by
labels and must become Available before the Workflow succeeds.

Services persist the application container port and Dockerfile path. Global
platform configuration supplies the OCI repository, rootless builder image,
registry-credential Secret name, preview URL scheme, and optional base domain.
An explicitly opt-in insecure-registry flag exists solely for local development
clusters; authenticated production registries retain secure transport by default.

When a base domain is configured, Argo also applies an Ingress with host
`<environment>.<base-domain>`. Without a base domain, the preview endpoint is
the in-cluster Service DNS URL. Ingress-controller and DNS provisioning remain
cluster-platform responsibilities.

### Convergence and URL publication

Each source generation persists its desired image and desired preview URL. The
generation-aware observation boundary established by ADR 0011 promotes all of
the following atomically only after Argo reports `Succeeded`:

- `desired_sha` to `deployed_sha`;
- desired image to deployed image; and
- desired preview URL to published preview URL.

Failed, erroneous, or stale Workflow observations publish none of those desired
values. The environment API therefore never advertises an endpoint for a
generation that has not converged.

## Consequences

- Preview environments execute repository-supplied container images rather than
  synthetic validation commands.
- Builds are registry-portable and independent of the SCM provider.
- Dockerfile ownership remains with the application team; language inference is
  not treated as a safe substitute for an explicit runtime contract.
- Route publication is deterministic, while external reachability depends on
  the configured ingress controller and DNS.
- The environment-management service account gains narrowly enumerated workload
  and routing verbs across dynamically created preview namespaces.
- Rootless BuildKit still requires relaxed seccomp/AppArmor profiles; clusters
  prohibiting those profiles must supply a policy-compatible builder image or a
  remote BuildKit service through configuration.

## Follow-up

- Capture and deploy the registry-returned image digest instead of relying only
  on a commit-SHA tag immutability policy.
- Add signed provenance, SBOM generation, vulnerability policy, and admission
  verification before workload deployment.
- Introduce per-service health paths, resource envelopes, and autoscaling policy.
- Discover load-balancer addresses and DNS readiness where the routing provider
  supports status APIs.
