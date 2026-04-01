ARG BASE_IMAGE=clawbernetes/openclaw:latest-base
FROM ${BASE_IMAGE}

# Install the observeclaw plugin (PII redaction, budget enforcement, policy).
RUN timeout 60 openclaw plugins install observeclaw || true

# Install the a2a-gateway plugin (Agent-to-Agent protocol).
RUN mkdir -p /home/node/.openclaw/workspace/plugins \
    && cd /home/node/.openclaw/workspace/plugins \
    && git clone --depth 1 https://github.com/win4r/openclaw-a2a-gateway.git a2a-gateway \
    && cd a2a-gateway \
    && npm install --production \
    && rm -rf .git
