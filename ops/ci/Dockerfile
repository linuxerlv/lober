FROM node:24.12.0-bookworm-slim@sha256:7326fb2dbdce998edd72140946851be64ef4a643e8715e138ca467e8e9d92c99 AS frontend
WORKDIR /src
RUN npm install --global pnpm@11.16.0
COPY web/ web/
WORKDIR /src/web
RUN --mount=type=cache,id=lober-pnpm,target=/pnpm/store,sharing=locked \
    pnpm install --frozen-lockfile --store-dir=/pnpm/store
RUN pnpm build:ele

FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS backend
WORKDIR /src
ARG GOPROXY
ENV GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,id=lober-gomod,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=frontend /src/internal/interfaces/console/ui/dist/ internal/interfaces/console/ui/dist/
RUN --mount=type=cache,id=lober-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=lober-gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/lober ./cmd/lober

FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend /out/lober /usr/local/bin/lober
ENTRYPOINT ["/usr/local/bin/lober"]
