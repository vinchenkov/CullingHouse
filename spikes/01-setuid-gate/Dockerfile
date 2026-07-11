# S1 setuid-gate spike image. The privileged setuid `mc` is BAKED IN
# (never bind-mounted) — the setuid bit must live on the image filesystem
# (spec §11.5). BUILD_NONCE exists so check.sh can force a genuine layer
# rebuild for the image-rebuild survival assertion.
FROM alpine:3.20

ARG BUILD_NONCE=0
RUN echo "build-nonce: ${BUILD_NONCE}" > /etc/build-nonce \
    && adduser -D -u 10001 mcpriv \
    && adduser -D -u 10002 agent

# Same static binary installed twice: once as the privileged setuid gate,
# once as a plain non-setuid copy the agent uses for the raw-open probes.
COPY probe/mcprobe /usr/local/bin/mc
COPY probe/mcprobe /usr/local/bin/agentprobe
RUN chown mcpriv:mcpriv /usr/local/bin/mc \
    && chmod 4755 /usr/local/bin/mc \
    && chown root:root /usr/local/bin/agentprobe \
    && chmod 0755 /usr/local/bin/agentprobe

CMD ["sleep", "infinity"]
