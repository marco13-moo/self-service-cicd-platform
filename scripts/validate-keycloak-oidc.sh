#!/usr/bin/env bash
set -euo pipefail

container=${OIDC_KEYCLOAK_CONTAINER:-self-service-cicd-keycloak}
image=${OIDC_KEYCLOAK_IMAGE:-quay.io/keycloak/keycloak:26.7.3}
port=${OIDC_KEYCLOAK_PORT:-58080}
admin_port=${OIDC_KEYCLOAK_ADMIN_PORT:-58081}
realm=platform-conformance
issuer="https://localhost:${port}/realms/${realm}"
keycloak_base="http://127.0.0.1:${admin_port}"
repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tls_directory=$(mktemp -d "$repository_root/.keycloak-tls.XXXXXX")

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -f "$tls_directory/tls.key" "$tls_directory/tls.crt"
  rmdir "$tls_directory" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup
mkdir -p "$tls_directory"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost \
  -addext subjectAltName=DNS:localhost,IP:127.0.0.1 \
  -keyout "$tls_directory/tls.key" -out "$tls_directory/tls.crt" >/dev/null 2>&1
docker run -d --name "$container" -p "127.0.0.1:${port}:8443" -p "127.0.0.1:${admin_port}:8080" \
  -v "$tls_directory:/tls:ro" \
  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=conformance-only \
  "$image" start-dev --http-enabled=true --hostname="https://localhost:${port}" \
    --https-certificate-file=/tls/tls.crt --https-certificate-key-file=/tls/tls.key >/dev/null
for _ in $(seq 1 600); do
  if [ "$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || true)" != true ]; then
    docker logs "$container" >&2
    exit 1
  fi
  if curl -fsS "${keycloak_base}/realms/master" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "${keycloak_base}/realms/master" >/dev/null

kcadm=(docker exec "$container" /opt/keycloak/bin/kcadm.sh)
"${kcadm[@]}" config credentials --server http://127.0.0.1:8080 --realm master --user admin --password conformance-only >/dev/null
"${kcadm[@]}" create realms -s realm="$realm" -s enabled=true -s accessTokenLifespan=4 >/dev/null
realm_id=$("${kcadm[@]}" get "realms/${realm}" --fields id --format csv --noquotes | tr -d '\r')
client_id=$("${kcadm[@]}" create clients -r "$realm" -i -s clientId=control-plane -s enabled=true -s publicClient=true -s directAccessGrantsEnabled=true)
"${kcadm[@]}" create "clients/${client_id}/protocol-mappers/models" -r "$realm" \
  -s name=audience -s protocol=openid-connect -s protocolMapper=oidc-audience-mapper \
  -s 'config."included.client.audience"=control-plane' -s 'config."access.token.claim"=true' >/dev/null
"${kcadm[@]}" create "clients/${client_id}/protocol-mappers/models" -r "$realm" \
  -s name=groups -s protocol=openid-connect -s protocolMapper=oidc-group-membership-mapper \
  -s 'config."claim.name"=groups' -s 'config."full.path"=false' -s 'config."access.token.claim"=true' >/dev/null
group_id=$("${kcadm[@]}" create groups -r "$realm" -i -s name=developers)
user_id=$("${kcadm[@]}" create users -r "$realm" -i -s username=alice -s enabled=true \
  -s firstName=Alice -s lastName=Conformance -s email=alice@example.test -s emailVerified=true)
"${kcadm[@]}" set-password -r "$realm" --userid "$user_id" --new-password conformance-only >/dev/null
"${kcadm[@]}" update "users/${user_id}/groups/${group_id}" -r "$realm" -n >/dev/null

token() {
  response=$(curl -sS -X POST "${keycloak_base}/realms/${realm}/protocol/openid-connect/token" \
    -d grant_type=password -d client_id=control-plane -d username=alice -d password=conformance-only \
  )
  access_token=$(printf '%s' "$response" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
  if [ -z "$access_token" ]; then
    echo "Keycloak did not issue an access token: $response" >&2
    return 1
  fi
  printf '%s' "$access_token"
}
validate() {
  (cd "$repository_root/control-plane" && GOCACHE=/tmp/self-service-cicd-go-cache go run ./cmd/oidc-conformance \
    -issuer "$issuer" -jwks-url "${issuer}/protocol/openid-connect/certs" -insecure-loopback-tls -token "$1" "${@:2}")
}

initial=$(token)
validate "$initial"
"${kcadm[@]}" create components -r "$realm" -s name=rotated-rsa -s parentId="$realm_id" \
  -s providerId=rsa-generated -s providerType=org.keycloak.keys.KeyProvider \
  -s 'config.priority=["200"]' -s 'config.enabled=["true"]' -s 'config.active=["true"]' \
  -s 'config.algorithm=["RS256"]' -s 'config.keySize=["2048"]' >/dev/null
rotated=$(token)
validate "$rotated"
validate "$initial"

"${kcadm[@]}" delete "users/${user_id}/groups/${group_id}" -r "$realm" >/dev/null
revoked=$(token)
validate "$revoked" -expect-failure
# Validator clock-skew tolerance is intentionally 30 seconds; prove expiry only
# after both the provider TTL and that bounded leeway have elapsed.
sleep 36
validate "$rotated" -expect-failure

echo "Keycloak OIDC issuance, JWKS rotation, group revocation, and token expiry conformance passed"
