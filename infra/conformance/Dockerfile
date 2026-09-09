# The production release pipeline publishes and signs this image, then replaces
# the local reference in the CronJob with its immutable registry digest.
FROM bitnami/kubectl@sha256:e0fcb25c485a8ad0665d0dd4b4a67cefb6baee32b0e3b19f54f1b468c49c1ed5

USER 0
COPY scripts/validate-cilium-tenant-isolation.sh /usr/local/bin/validate-cilium-tenant-isolation
RUN chmod 0555 /usr/local/bin/validate-cilium-tenant-isolation

USER 1001
ENTRYPOINT ["/usr/local/bin/validate-cilium-tenant-isolation"]
