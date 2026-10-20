# Spec Reader

A small, self-hosted OpenAPI catalog and API explorer. One instance can group many OpenAPI 3.x documents, search their operations, authenticate with a configured login preset, and send requests through a guarded server-side proxy.

## Run the example

```sh
go run ./cmd/spec-reader -config examples/petstore/config.yaml
```

Open `http://localhost:3013`. For the full UI, run `pnpm install && pnpm build` in `web/` first, or use Docker:

```sh
docker build -t spec-reader .
docker run --rm -p 3013:3013 \
  -v "$PWD/examples/petstore:/config:ro" \
  spec-reader -config /config/config.yaml
```

## Configuration

See [`examples/petstore/config.yaml`](examples/petstore/config.yaml). Each spec needs a stable `id`, a local `file`, and optional `group`, `production`, and `tryIt` values. The proxy only permits origins declared by a loaded spec or `proxy.extraOrigins`; private, loopback, link-local, and Tailscale addresses are rejected unless `allowPrivateNetworks` is explicitly enabled.

Authentication presets describe a JSON login call and paths to the returned access token, refresh token, and expiry. Tokens remain in the browser. Session storage is the default; users can explicitly opt into local storage.

## Security

The proxy pins connections to resolved public IPs, rejects unlisted origins, requires same-origin requests with `X-Spec-Reader: 1`, strips cookies and hop-by-hop headers, limits bodies to 10 MB, times requests out, and rate-limits each client IP. Non-GET requests to configurations marked `production` require browser confirmation.

## License

MIT
