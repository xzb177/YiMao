ARG BASE_IMAGE
FROM ${BASE_IMAGE}
HEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=2 CMD true
