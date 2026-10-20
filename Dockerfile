FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
COPY internal/server/dist/ ../internal/server/dist/
RUN pnpm build

FROM golang:1.24-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/server/dist ./internal/server/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /spec-reader ./cmd/spec-reader

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /spec-reader /spec-reader
EXPOSE 3013
ENTRYPOINT ["/spec-reader"]
