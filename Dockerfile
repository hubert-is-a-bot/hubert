# Hubert runner image.
#
# The image is what every dispatched Kubernetes Job runs. It
# ships:
#   - hubert-runner on $PATH
#   - at least one LLM CLI (claude by default; opencode /
#     gemini added by image variants when wanted)
#   - gh for GitHub API calls
#   - git for clone / commit / push
#
# Build with:
#   docker build -t hubert-runner:dev .
#
# The deployment passes the image ref through the
# HUBERT_IMAGE repo variable consumed by
# hubert-orchestrator.yml; the Job manifest template picks it
# up via dispatch.Config.Image.

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/hubert-runner ./cmd/hubert-runner \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/hubert-dispatch ./cmd/hubert-dispatch \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/hubert-snap ./cmd/hubert-snap \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/hubert-prompts ./cmd/hubert-prompts

FROM debian:bookworm-slim
ARG CLAUDE_CLI_VERSION=latest
RUN set -eux \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      gnupg \
      nodejs \
      npm \
 && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      | gpg --dearmor -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
      > /etc/apt/sources.list.d/github-cli.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends gh \
 && npm install -g "@anthropic-ai/claude-code@${CLAUDE_CLI_VERSION}" \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/hubert-runner /usr/local/bin/hubert-runner
COPY --from=build /out/hubert-dispatch /usr/local/bin/hubert-dispatch
COPY --from=build /out/hubert-snap /usr/local/bin/hubert-snap
COPY --from=build /out/hubert-prompts /usr/local/bin/hubert-prompts

RUN groupadd -g 10000 hubert \
 && useradd -u 10000 -g 10000 -m -d /opt/data hubert \
 && chown -R hubert:hubert /opt/data

USER hubert
WORKDIR /opt/data
ENV HOME=/opt/data

ENTRYPOINT ["/usr/local/bin/hubert-runner"]
