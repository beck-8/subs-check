FROM alpine AS builder

ARG VERSION
ARG GITHUB_SHA

WORKDIR /app

ENV TZ=Asia/Shanghai

RUN apk add --no-cache \
    alpine-conf \
    ca-certificates \
    nodejs \
    zstd \
    curl && \
    /usr/sbin/setup-timezone -z Asia/Shanghai && \
    mkdir -p assets && \
    ARCH=$(uname -m) && \
    case "$ARCH" in \
        x86_64) \
            ARCH_NAME=x86_64 \
            ;; \
        aarch64) \
            ARCH_NAME=aarch64 \
            ;; \
        armv7l) \
            ARCH_NAME=armv7 \
            ;; \
        *) \
            echo "Unsupported architecture: $ARCH" && exit 1 \
            ;; \
    esac && \
    APP_VERSION=${VERSION#v} && \
    echo "Downloading for version ${VERSION} ${ARCH_NAME} (commit: ${GITHUB_SHA:0:7})" && \
    curl -L -o /tmp/binary.tar.gz "https://github.com/beck-8/subs-check/releases/download/${VERSION}/subs-check_Linux_${ARCH_NAME}.tar.gz" && \
    tar xzf /tmp/binary.tar.gz -C .

FROM alpine
WORKDIR /app
ENV TZ=Asia/Shanghai
ENV NODEBIN_PATH=/usr/bin/node
RUN apk add --no-cache alpine-conf ca-certificates nodejs &&\
    /usr/sbin/setup-timezone -z Asia/Shanghai && \
    apk del alpine-conf && \
    rm -rf /var/cache/apk/* 
COPY --from=builder /app/subs-check /app/subs-check
CMD ["/app/subs-check"]
EXPOSE 8199
EXPOSE 8299