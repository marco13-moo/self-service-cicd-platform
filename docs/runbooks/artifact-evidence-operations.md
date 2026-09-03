# Artifact Evidence Operations Runbook

## Trust-root rotation

### Preconditions

- The candidate KMS key is non-exportable and its signing authorization is bound
  only to the Argo execution identity.
- `preview-cosign-public-candidate` contains only the candidate public key.
- Registry backup and admission replay have passed within the current change
  window.
- An incident owner and rollback deadline are recorded.

### Procedure

1. Apply `infra/k8s/kyverno-preview-cosign-key-role.yaml` so Kyverno can read the
   two explicitly named public-key Secrets.
2. Apply `infra/k8s/preview-image-signature-policy-rotation.yaml`. Confirm that a
   subject signed by the current root remains admissible.
3. Change `PREVIEW_COSIGN_SIGNER` to the candidate KMS URI and restart the control
   plane so newly submitted Workflows receive the new signer identity.
4. Build a fresh immutable subject. Require successful image signing, signed
   policy attestation, direct verification, rollout, and server-side admission.
5. Copy the candidate public key into `preview-cosign-public`, reapply
   `infra/k8s/preview-image-signature-policy.yaml`, and verify both the fresh
   subject and every retained running subject.
6. Remove `preview-cosign-public-candidate` only after the observation window.
   Disable the previous KMS signing grant after the rollback deadline; retain its
   public key wherever historical evidence remains within policy.

If any checkpoint fails before step 5, restore the prior signer configuration,
reapply the non-rotation policy, and retain the candidate key for investigation.
Do not delete either KMS key during the incident window.

## Quarantine response

The scheduled re-verifier fails its Workflow after containment. Alert on that
failure and inspect these fields:

- namespace label `platform.preview-quarantine=true`;
- namespace annotations `platform.preview-quarantine-reason` and
  `platform.preview-quarantine-observed-at`; and
- Deployment annotation `platform.preview-quarantine-image`.

Do not merely restore replicas. Determine whether the cause is evidence loss,
trust-root removal, registry unavailability, or unauthorized mutation. Restore
service using a newly verified digest or recovered evidence, remove quarantine
metadata in an audited change, and then restore the intended replica count.

## Registry recovery

Run `scripts/validate-registry-evidence-lifecycle.sh` before accepting a registry
backup mechanism or changing its garbage-collection policy. Production recovery
must use the provider's immutable backup rather than the harness's disposable
registries, but must repeat the same assertions:

1. restore into an empty registry endpoint;
2. compare the subject and BuildKit attestation-manifest digests;
3. locate both SPDX and SLSA provenance predicates;
4. verify the Cosign image signature and vulnerability-policy attestation; and
5. replay admission against the recovered digest.

Record measured RPO/RTO and the immutable subjects in `docs/evidence/`. Never
include credentials, Vault tokens, authorization headers, or private key data.
