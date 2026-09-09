#!/usr/bin/env bash
set -euo pipefail

# Build once, publish immutable artifacts, and bind all supply-chain evidence to
# the resulting digest. No promotion may reconstruct these images.
for command in docker cosign syft trivy jq; do
  command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done
: "${RELEASE_REGISTRY:?set RELEASE_REGISTRY, for example registry.example/platform}"
: "${RELEASE_VERSION:?set RELEASE_VERSION to an immutable release identifier}"
: "${COSIGN_KEY:?set COSIGN_KEY to a KMS URI or workload-identity key reference}"

case "$RELEASE_VERSION" in *[!A-Za-z0-9._-]*|'') echo "invalid RELEASE_VERSION" >&2; exit 1;; esac
evidence_dir="${EVIDENCE_DIR:-release-evidence/$RELEASE_VERSION}"
mkdir -p "$evidence_dir"

release_one() {
  local name=$1 dockerfile=$2
  local tagged="$RELEASE_REGISTRY/$name:$RELEASE_VERSION"
  docker buildx build --provenance=false --file "$dockerfile" --tag "$tagged" --push .
  local digest
  digest=$(docker buildx imagetools inspect "$tagged" --format '{{json .Manifest.Digest}}' | tr -d '"')
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "registry returned invalid digest for $name" >&2; exit 1; }
  local immutable="$RELEASE_REGISTRY/$name@$digest"
  syft "$immutable" -o spdx-json="$evidence_dir/$name.sbom.spdx.json"
  trivy image --exit-code 1 --severity CRITICAL --ignore-unfixed "$immutable"
  jq -n --arg name "$name" --arg digest "${digest#sha256:}" '{_type:"https://in-toto.io/Statement/v1",subject:[{name:$name,digest:{sha256:$digest}}],predicateType:"https://slsa.dev/provenance/v1",predicate:{buildDefinition:{buildType:"https://github.com/docker/buildx"},runDetails:{builder:{id:"self-service-cicd-platform"}}}}' >"$evidence_dir/$name.provenance.json"
  cosign attest --yes --key "$COSIGN_KEY" --type spdxjson --predicate "$evidence_dir/$name.sbom.spdx.json" "$immutable"
  cosign attest --yes --key "$COSIGN_KEY" --type slsaprovenance --predicate "$evidence_dir/$name.provenance.json" "$immutable"
  cosign sign --yes --key "$COSIGN_KEY" "$immutable"
  jq -n --arg image "$immutable" --arg sbom "$evidence_dir/$name.sbom.spdx.json" '{image:$image,sbom:$sbom}'
}

control=$(release_one control-plane Dockerfile)
conformance=$(release_one network-conformance infra/conformance/Dockerfile)
jq -n --arg version "$RELEASE_VERSION" --argjson control "$control" --argjson conformance "$conformance" \
  '{schema:"platform.release/v1",version:$version,artifacts:{control_plane:$control,network_conformance:$conformance}}' >"$evidence_dir/release.json"
cosign sign-blob --yes --key "$COSIGN_KEY" --bundle "$evidence_dir/release.bundle.json" "$evidence_dir/release.json"
echo "published signed release manifest: $evidence_dir/release.json"
