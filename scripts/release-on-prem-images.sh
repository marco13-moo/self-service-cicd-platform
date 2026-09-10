#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${ON_PREM_RELEASE_VERSION:-on-prem-$(date -u +%Y%m%d%H%M%S)}
evidence_dir=${ON_PREM_EVIDENCE_DIR:-$root/certification-evidence/$version}
secret_dir=${ON_PREM_SECRET_DIR:-$root/.on-prem-secrets}
harbor_local_port=${HARBOR_LOCAL_PORT:-5002}
for command in docker kubectl jq syft trivy cosign skopeo; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done
[[ -s "$secret_dir/harbor-admin" ]] || { echo "bootstrap the on-prem profile first" >&2; exit 1; }
mkdir -p "$evidence_dir"
# Skopeo's docker-daemon transport does not honor Docker contexts by itself.
# Resolve the active endpoint once so Colima, Docker Desktop, and Linux Docker
# all feed the exact daemon used by the preceding build.
export DOCKER_HOST
DOCKER_HOST=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
harbor_node_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' self-service-cicd-cilium-worker2)
push_registry="127.0.0.1:${harbor_local_port}"
cluster_registry="${harbor_node_ip}:30002"

forward_pid=
if [[ ${HARBOR_PORT_FORWARD:-true} == true ]]; then
  kubectl -n harbor port-forward --address 127.0.0.1 service/harbor "${harbor_local_port}:80" >"$evidence_dir/harbor-port-forward.log" 2>&1 &
  forward_pid=$!
fi
trap '[[ -z "$forward_pid" ]] || kill "$forward_pid" >/dev/null 2>&1 || true' EXIT
for _ in $(seq 1 60); do curl -fsS "http://$push_registry/api/v2.0/health" >/dev/null 2>&1 && break; sleep 1; done
curl -fsS "http://$push_registry/api/v2.0/health" >/dev/null
[[ -z "$forward_pid" ]] || kill -0 "$forward_pid"
registry_auth_dir="$secret_dir/registry-auth"
mkdir -p "$registry_auth_dir"
chmod 0700 "$registry_auth_dir"
registry_auth=$(printf 'admin:%s' "$(cat "$secret_dir/harbor-admin")" | base64 | tr -d '\n')
jq -n --arg registry "$push_registry" --arg auth "$registry_auth" \
  '{auths:{($registry):{auth:$auth}}}' >"$registry_auth_dir/config.json"
chmod 0600 "$registry_auth_dir/config.json"
if [[ ! -s "$secret_dir/release-signing.key" ]]; then
  umask 077
  export COSIGN_PASSWORD
  COSIGN_PASSWORD=$(openssl rand -base64 32 | tr -d '\n')
  printf '%s' "$COSIGN_PASSWORD" >"$secret_dir/release-signing.password"
  cosign generate-key-pair --output-key-prefix "$secret_dir/release-signing" >/dev/null
else
  export COSIGN_PASSWORD
  COSIGN_PASSWORD=$(cat "$secret_dir/release-signing.password")
fi

release_one() {
  local name=$1 dockerfile=$2 result_file=$3
  local tag="on-prem-release/$name:$version"
  docker build --file "$dockerfile" --tag "$tag" "$root" >/dev/null
  skopeo copy --src-daemon-host "$DOCKER_HOST" --dest-tls-verify=false --dest-creds "admin:$(cat "$secret_dir/harbor-admin")" "docker-daemon:$tag" "docker://$push_registry/library/$name:$version" >/dev/null
  local digest
  digest=$(skopeo inspect --tls-verify=false --creds "admin:$(cat "$secret_dir/harbor-admin")" "docker://$push_registry/library/$name:$version" | jq -er '.Digest')
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "invalid digest for $name" >&2; exit 1; }
  local image="$push_registry/library/$name@$digest"
  syft "$image" -o "spdx-json=$evidence_dir/$name.sbom.spdx.json" >/dev/null
  # The reference Harbor endpoint is deliberately HTTP-only inside the disposable
  # certification topology; production profiles remain TLS-mandatory.
  trivy image --insecure --exit-code 1 --severity CRITICAL --ignore-unfixed --format json --output "$evidence_dir/$name.trivy.json" "$image"
  jq -n --arg name "$name" --arg digest "${digest#sha256:}" --arg version "$version" \
    '{buildType:"on-prem-reference/docker",externalParameters:{version:$version},subject:[{name:$name,digest:{sha256:$digest}}]}' >"$evidence_dir/$name.provenance.json"
  DOCKER_CONFIG="$registry_auth_dir" cosign attest --yes --allow-insecure-registry --key "$secret_dir/release-signing.key" --type spdxjson --predicate "$evidence_dir/$name.sbom.spdx.json" "$image" >/dev/null
  DOCKER_CONFIG="$registry_auth_dir" cosign attest --yes --allow-insecure-registry --key "$secret_dir/release-signing.key" --type slsaprovenance --predicate "$evidence_dir/$name.provenance.json" "$image" >/dev/null
  DOCKER_CONFIG="$registry_auth_dir" cosign sign --yes --allow-insecure-registry --key "$secret_dir/release-signing.key" "$image" >/dev/null
  jq -n --arg external "$image" --arg internal "$cluster_registry/library/$name@$digest" --arg digest "$digest" \
    '{image:$external,cluster_image:$internal,digest:$digest}' >"$result_file"
}

release_one control-plane Dockerfile "$evidence_dir/control-plane.release.json"
release_one network-conformance infra/conformance/Dockerfile "$evidence_dir/network-conformance.release.json"
control=$(cat "$evidence_dir/control-plane.release.json")
conformance=$(cat "$evidence_dir/network-conformance.release.json")
jq -n --arg version "$version" --arg profile on-prem-reference --argjson control "$control" --argjson conformance "$conformance" \
  '{schema:"platform.release/v1",certification_tier:$profile,version:$version,artifacts:{control_plane:$control,network_conformance:$conformance}}' >"$evidence_dir/release.json"
cosign sign-blob --yes --key "$secret_dir/release-signing.key" --bundle "$evidence_dir/release.bundle.json" "$evidence_dir/release.json" >/dev/null
echo "$evidence_dir/release.json"
