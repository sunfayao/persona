# Direct public access to hub

Phones connect directly to hub over the public internet, preferring IPv6; users configure their own VPN. Hub provides a separate TLS listener and retains HTTP for LAN / old clients. **Expose only the TLS port publicly**; clients must verify HTTPS / WSS before sending tokens.

This document reflects existing `server/internal/hub/` and `server/internal/netaddr/` implementations. Separate DDNS and doctor implementations are explained below.

<a id="hubjson-网络配置"></a>

## hub.json network configuration

Merge these fields into `~/.persona/hub.json` on the hub machine, preserving existing token, database, and machine configuration, then restart hub:

```json
{
  "listen": ":18660",
  "public_url": "http://hub.example.com:18660",
  "public": {
    "tls_listen": ":18662",
    "advertise": ["https://hub.example.com:18662"],
    "advertise_ipv6": true,
    "advertise_lan": true
  },
  "security": {
    "trusted_cidrs": [],
    "allow_public_plaintext": false
  }
}
```

Domains are examples; replace with your configuration. HTTP addresses should be reachable over LAN / VPN. `18662` is only this document's example TLS port; no TLS port is built in by default. Avoid HTTP / Postgres port conflicts. Configuration changes need a restart; certificate refresh behavior is described next.

| Field | Default and behavior |
| --- | --- |
| `listen` | HTTP default `:18660`, retained for LAN / VPN. The hub's own commands on its machine (`hub pair`, `hub admin`, `hub peer`, `hub setup`, `hub doctor`, `personad extension`) call it from loopback, where it takes their master token: on `127.0.0.1` when it listens on every address or `listen` names a host, at the address `listen` names when that is loopback or one of this machine's. An address the machine doesn't have (a DHCP lease that moved on) is never called, so the token stays on the machine |
| `public_url` | Primary pairing URL (see the [local app QR exception](../protocol/protocol.md#temporary-pairing)), also added to candidates; keep an address old clients can reach |
| `public.tls_listen` | Empty string disables TLS; setting it adds separate HTTPS / WSS listening |
| `public.cert_file` / `public.key_file` | Both empty means managed self-signed certificates; custom certificates require both file paths, preferably absolute |
| `public.advertise` | Additional candidates; full HTTPS URLs recommended, also accepts domains, IPs, or hosts with ports. Missing scheme / port is filled from current listener. Advertising does not start listeners, open firewalls, or update DNS |
| `public.advertise_ipv6` | Unset or `true` automatically advertises local global IPv6 candidates |
| `public.advertise_lan` | Unset or `true` automatically advertises LAN IPv4 / ULA candidates, delivered only after authentication |
| `public.ddns` | Omitted disables it; use a domain to recover hub after prefix changes; see [configuration](config.md#ddnspublicddns) |

Both listeners share header timeout, idle-connection timeout, and header-size limits. No whole-request read / write timeout, avoiding truncating WebSockets or large-file transfers. Instead a request body that sends nothing for 2 minutes is answered and let go: over HTTP/1 its connection is closed; over HTTP/2, which clients of the TLS listener usually negotiate, only that request's stream ends. A slow upload that keeps moving is not cut (`server/internal/hub/bodyidle.go`).

The two `advertise_*` switches control only discovery, not explicitly configured `public_url` / `public.advertise`. With TLS enabled, discovered candidates use HTTPS and the TLS port; explicit HTTP URLs remain unchanged.

The following values are policy defaults from `config_security.go`, **not measured throughput, performance, or protection claims**. Zero / negative integers use defaults.

| `security` field | Default | Meaning |
| --- | --- | --- |
| `trusted_cidrs` | `[]` | Additional trusted source subnets, treated as LAN on match; include only networks you truly control |
| `allow_public_plaintext` | `false` | Allows public plaintext API when enabled, with a startup warning; does not remove the public main-token prohibition |
| `plaintext_loopback_only` | `false` | Refuses plaintext from every source but loopback, the LAN and `trusted_cidrs` included: pages redirect to the TLS listener, everything else gets `403 tls_required`. Needs `public.tls_listen`; see [Keeping tokens off the LAN](#keeping-tokens-off-the-lan) |
| `auth_failures` | `10` | Ban source after this many failures within a counting window |
| `auth_window_seconds` | `60` | Window from the source's first failure in this round, seconds |
| `auth_block_seconds` | `900` | Ban duration after threshold, seconds |
| `auth_max_sources` | `4096` | Source counter capacity; clear expired entries when full, then evict unbanned entries with earliest expiry first |
| `auth_cache_entries` | `2048` | Device-token lookup cache capacity, including invalid tokens; code fixes TTL at 30 seconds |
| `auth_log_interval_seconds` | `30` | Minimum security-error logging interval, with skipped counts aggregated in `suppressed` |
| `proxy_protocol` | Omitted; reads no PROXY headers | Preserves real source classification behind same-machine port forwarding / TCP passthrough; see [PROXY protocol](#proxy-protocol) below |

Public IPv4 counts per address; IPv6 aggregates by /64. Ban checks precede token database lookup, so even valid tokens from banned sources get `429` and a seconds-valued `Retry-After`. The threshold-triggering failure still returns `401`; subsequent requests return `429`. LAN / VPN sources are not counted.

## TLS and public-key pinning

Without custom certificate files, hub creates ECDSA P-256 identity key `hub.key` and self-signed `hub.crt` in `~/.persona/tls/`. Directory permissions 0700, files 0600. The certificate contains only `<hub_id>.persona.invalid`, no local hostname or LAN addresses. Minimum TLS version 1.2.

Managed certificates last 365 days, renewed with the same key when roughly less than 30 days remain; also reissued if not yet valid or SAN mismatches. Hub certificate refresh timer is 2 minutes, a scheduling configuration, **not a measured refresh delay**. Custom certificates are reread, but hub does not obtain or renew them; no automatic ACME flow currently.

Run on the hub machine:

```sh
~/.persona/bin/personad hub pair
```

With TLS enabled, the printed "Public key fingerprint" is pin `fp`: SHA-256 of certificate SubjectPublicKeyInfo DER, unpadded base64url, 43 characters. QR codes carry it too. `hub pair` generates a new temporary token; protect links / QR codes as credentials. It is not a read-only fingerprint command.

Pinning-capable clients obtain `fp` through trusted scanning, verify the public key in TLS handshake, and check `hub_id`. Already-paired clients can read `hello` or `/api/v1/endpoints` over a trusted authenticated LAN channel. **The `fp` in public unauthenticated health responses cannot establish initial trust.**

Replacing a certificate while keeping the private key preserves the pin. Replacing the private key changes it, requiring all pinned devices to re-pair. A running hub refuses detected key replacements and logs them; restart required. Deleting `tls/` is not a normal renewal step. No `fp_next` announcement or seamless key rotation currently.

Default pairing tokens last 10 minutes and redeem a device token once. After redemption they remain at most 2 more minutes, never beyond original expiry. This supports old iOS pairing; see [protocol](../protocol/protocol.md#临时配对). Only `hub pair --legacy` prints the main token; public sources always reject it.

Computer links (persona-link, [link.md](link.md)) also use only this TLS port and pin the fingerprint obtained at pairing, including on LAN; hub rejects their plaintext requests except loopback.
TLS terminates at hub, which authenticates link credentials and forwards the connection unchanged to the computer-connection manager (2026-11-04, [machines.md](../server/machines.md), "Who connects to computers").
Forwarding uses `GET /api/v1/bus/relay` with the manager's own bus token (same as the bus: only `Authorization: Bearer jb_…`, TLS required for public sources; device credentials cannot open it, nor can it open app endpoints).

## Keeping tokens off the LAN

By default the plaintext `listen` port accepts tokens from any LAN or VPN source. On a network other people share, such as an office LAN or a company VPN, anyone who can capture traffic there can read them. Setting `security.plaintext_loopback_only: true` limits plaintext to the hub machine itself. For a plaintext request from any other source:

- A page request (`GET` / `HEAD` outside `/api/` and `/i/`) gets `308` to the same path and host name on the TLS port. Links built from an `http://` `public_url` (`/s/...`, `/todos?id=...`) and bookmarks keep working, and the browser reaches the app over HTTPS before any script reads a stored token.
- Everything else, `/api/v1/health` included, gets `403 tls_required`.

Loopback keeps plaintext. That covers the built-in manager, the local assistant, SSH tunnels that end on the hub machine, and a reverse proxy on the hub machine that terminates TLS. The hub refuses to start with the switch on and no `public.tls_listen`. If the TLS address has gone at start-up (see above), only the hub machine can reach the hub until it is fixed. The setting does not change the listening sockets or `personad.socket`, so it takes effect at the next restart or `personad hub switch`.

Before turning it on, make sure every client can use the TLS port:

| Client | Over the TLS port |
| --- | --- |
| Persona Link, mesh peers | Already TLS-only, pinned |
| Web app | Works after the redirect. The browser warns about a certificate it does not trust, and `localStorage` is per origin, so the first visit on HTTPS asks for sign-in again |
| ssh / tailcat nodes, the built-in manager | Not affected: they carry no hub token over the hub's HTTP port |

The managed self-signed certificate names only `<hub_id>.persona.invalid`, so no system trust store accepts it. To keep clients that only use the system trust store working, give the TLS listener a certificate they trust through `public.cert_file` / `public.key_file`. One option is a publicly trusted certificate for a name that resolves to the hub. Another is a private CA that you install and trust on each device. Issue the certificate for the managed key `~/.persona/tls/hub.key`: the pin is the public key's hash, so Persona Link and other pinned clients keep working without pairing again. With a private CA, for example:

```sh
cd ~/.persona/tls
openssl req -new -key hub.key -subj "/CN=<hub address>" -out hub.csr
printf 'subjectAltName=IP:<hub address>\nextendedKeyUsage=serverAuth\n' > hub.ext
openssl x509 -req -in hub.csr -CA <ca>.crt -CAkey <ca>.key -CAcreateserial -days 825 -sha256 -extfile hub.ext -out hub-ca.crt
```

Install the CA on each device and trust it. Then set `public_url` to the `https://` address so that new pairings and links use it. Pair each device again, and remove its old device so that its old token stops working. The hub does not renew a custom certificate, so reissue it before it expires. Persona Link pins the key, which does not change.

## Candidate addresses and DDNS

Candidates sort `ipv6`, `name`, `lan`, `ipv4`, HTTPS first within each type. Linux reads flags and lifetimes through netlink, normally excluding temporary, deprecated, tentative, DAD-failed, and expired global IPv6, virtual interfaces, link-local, and documentation prefixes. ULA is `lan`. Public IPv4 is advertised only by explicit configuration; no automatic NAT discovery or inbound public-IPv4 verification currently.

On non-Linux platforms without flags, or ordinary interface enumeration after netaddr failure, hub cannot guarantee temporary IPv6 exclusion. `netaddr.Stable` rejects unknown flags itself, but hub's fallback enumeration accepts them into candidates; do not call these "verified stable addresses."

Address-change notifications trigger rescans, with periodic fallback. Candidates mean an address was found locally or configured, **not that it is publicly reachable**. `seen_at` is collection time, not connectivity-test time. Clients obtain lists through authenticated `/api/v1/endpoints` and WS `hello`; only connections declaring `endpoints` receive address-change events.

Broadband reconnection may change IPv6 prefixes, invalidating existing connections / literal addresses. Recover with a domain: `public.ddns` makes hub update DNS (see [configuration](config.md#ddnspublicddns)), or use an external DDNS tool for AAAA records. Either way, add the HTTPS domain URL with TLS port to `public.advertise` so the app receives it; `advertise` alone does not update DNS.

### Client-added candidates

Hub's advertised list may be incomplete: discovery may miss addresses on physical interfaces classified as virtual, or `public.advertise` may be unconfigured. Clients can add candidates:

- Manual candidates stay on the client, saved per pairing. New hub lists replace only hub-supplied candidates, not manual ones, client-disabled settings, or user ordering.
- Clients may let users reorder candidates and try in that exact order: displayed order equals attempt order, without per-network memory or hidden adjustments. New hub candidates append; removed hub candidates disappear.
- Same security requirements as hub-supplied candidates: HTTPS pins the public-key fingerprint from trusted pairing and checks `hub_id`. Pairings without fingerprints (hub itself has no TLS, behind a reverse proxy) use system CA / hostname verification for HTTPS; see "Reverse-proxy subpaths" below. Plain HTTP is LAN-only; check the actual peer address again before sending a token.
- Manual candidates do not prove reachability, just as `seen_at` is not a connectivity test. Clients may probe unauthenticated `GET /api/v1/health` only as evidence "This address reaches the already-pinned hub," never deriving initial trust from response fingerprints.

Computer links: [link.md](link.md).

## Allowing IPv6 ingress

First confirm a physical hub interface has global IPv6, keeping a stable address suitable for long-term advertisement. Do not use a privacy temporary address as a fixed service address. Prefer randomly generated stable interface identifiers to easily guessed fixed suffixes.

Modem / router menus vary, usually "IPv6 firewall," "Inbound rules," or "IPv6 access control." Configure each layer in the actual topology:

1. If the modem routes, add an inbound allow rule for hub's global IPv6 with **TCP destination port = TLS port**. If another router firewall follows, allow both layers. With bridged modem, inspect the dialing router instead.
2. Target the hub device or its stable IPv6, allowing only example TCP 18662. IPv6 direct access normally uses firewall rules, not copied IPv4 NAT port mapping. After prefix changes, verify rules target the current address.
3. Keep other inbound ports closed: no HTTP 18660, Postgres 18661, or other management services. If equipment only supports disabling the entire IPv6 firewall, first ensure every home host has inbound protection; protecting hub alone does not secure the network.
4. Hub's host firewall must also allow the TCP TLS port, retaining default-deny for public ingress. Keep SSH management / LAN HTTP under existing trusted-source rules. Verify IPv6 rules, not just IPv4.

Docker published ports also need container-forwarding rule checks; ufw's inbound list alone does not prove a port is unexposed. Keep database loopback-bound. Preserve a working management connection while changing firewalls.

After allowing ingress, disable phone Wi-Fi and test on cellular, confirming IPv6 support there. Accessing hub's own address from hub follows a local path and cannot prove modem rules work. Public reachability, latency, and carrier idle-connection lifetime were not measured for this document.

<a id="来源判断与反向代理"></a>

## Source classification and reverse proxies

Hub trusts only connection source and local receiving addresses, **not `X-Forwarded-For` or `Forwarded` for trust decisions**; the sole exception is PROXY headers from configured upstreams (see [PROXY protocol](#proxy-protocol) below). Loopback, private / ULA, link-local, IPv6 prefixes on the same local-interface link (/64 or longer), and `trusted_cidrs` matches are local-network sources. Shared carrier IPv4 space is not automatically trusted; only connections arriving on local VPN interfaces or explicitly whitelisted count as LAN.

**Container-gateway exception**: in containers, the gateway (default-route next hop, first address in each subnet, `server/internal/hub/admin_gateway.go`) counts as public, not local, unless in `trusted_cidrs`. Docker userspace proxy (docker-proxy) may make LAN and public connections alike appear from the gateway; without exclusion, public requests could use main tokens / plaintext. LAN connections through Linux iptables DNAT retain source addresses and are unaffected, as are `--network host` deployments.

**Docker Desktop / OrbStack forwarded sources** are not guaranteed gateways: Docker Desktop gvisor networking may make local and LAN clients share one public-looking address. Hub uses the actual peer, so plaintext APIs return `403 tls_required`; whitelisting only `docker network inspect`'s gateway may not help. While connecting, inspect the remote address for container port 18660 with `docker compose exec hub netstat -tn`, then follow [Docker troubleshooting](config.md). Prefer hub TLS. For local-only trials, bind host ports to `127.0.0.1`, then whitelist the observed forwarded source as a single-address CIDR in `security.trusted_cidrs`. That source may represent all forwarded clients; a whitelist does not restore original identity.

Local TLS-terminating reverse proxies make source loopback; remote ones make it the proxy address. This affects public plaintext / main-token restrictions, health redaction, and rate limits. Forwarded headers cannot restore these checks. A reverse proxy must handle public TLS, access control, rate limiting, and prevent public access through it to local-only pairing-code endpoints. Prefer hub's own TLS listener. TCP-only forwarding without TLS termination (port forwarding, tunneling, nginx `stream` SNI passthrough to TLS port) can use PROXY protocol to convey real sources.

### PROXY protocol

**Risk**: with a **same-machine** TCP-forwarding layer (for example, a public tunnel to localhost, then nginx `stream` SNI passthrough to hub TLS), every public connection appears from `127.0.0.1` and is treated as local:

- admin UI bypasses `admin.access` (loopback always allowed), even publicly with `cidrs` configured;
- main token works publicly;
- only public authentication failures count, so public token / admin-password guessing is unthrottled;
- logs show only `127.0.0.0/24`, concealing real sources.

TLS passthrough cannot add HTTP headers, and forwarding headers must not grant access. PROXY protocol ([specification](https://www.haproxy.org/download/2.9/doc/proxy-protocol.txt)) prepends a text line (v1) or binary header (v2) before TLS handshake, stating whose connection is forwarded. Hub supports v1 and v2, trusting only configured upstreams:

```json
{
  "security": {
    "proxy_protocol": {
      "from": ["127.0.0.1/32"],
      "listeners": ["tls"]
    }
  }
}
```

| Field | Default | Meaning |
| --- | --- | --- |
| `from` | Required | Trusted upstream CIDRs or single addresses, as seen by hub (typically `127.0.0.1/32` for same-machine). Include only the proxy itself; `0.0.0.0/0` / `::/0` rejected |
| `listeners` | `["tls"]` | Applies to `tls` (`public.tls_listen`) / `http` (`listen`). If `from` includes loopback, `http` is forbidden: local `personad hub pair`, `hub admin`, and plugins use plaintext loopback without headers and would be rejected |

Without `proxy_protocol`, hub reads no PROXY headers and behaves exactly as before. Invalid configuration (empty / nonaddress `from`, unknown `listeners`, HTTP with loopback) prevents startup; `personad hub doctor` identifies the field. TLS-only without `public.tls_listen` logs a notice. Restart after changing.

**Rules** (`server/internal/proxyproto`):

- **Connections from `from` require valid headers**; header source becomes the peer for source classification, admin access, main-token restrictions, rate limits, and logs. Missing, invalid, or timed-out headers close without response; otherwise bypassing the proxy (or a misconfigured proxy without PROXY support) would again become loopback.
- **PROXY headers from other sources are not accepted**: hub does not read them; bytes remain the connection prefix, causing TLS failure or HTTP 400. Existing clients unaffected; sources outside `from` cannot impersonate addresses.
- **v2 `LOCAL`** (proxy-originated connections, such as health checks) uses the socket's source, the upstream address, per specification.
- **v1 `UNKNOWN`, v2 `PROXY` with `UNSPEC` family** (upstream does not know the source, e.g. received over Unix socket) are rejected. Although the specification permits socket-address fallback, that would use upstream loopback and trust forwarded traffic as local. v2 UDP / Unix families also rejected.
- **Loopback header source with nonloopback upstream** (another machine forwarding its local programs) uses the upstream address; that machine's "local" is not hub-local.
- **Timeout / size**: complete header within 5 seconds (unmeasured policy default); v1 at most 107 bytes (spec limit), v2 address + TLV at most 4096 bytes, TLVs discarded after reading. Read on the connection's own goroutine at first read / write / peer lookup, never in Accept, so slow / silent connections do not block others.
- **Local address remains the socket's**, not header destination, which belongs to the upstream (proxy listener, possibly another machine). Hub uses local address to determine entry through a local VPN interface (`100.64.0.0/10` classification). Proxied shared-space sources are therefore public—stricter, never looser; add `trusted_cidrs` if needed.
- Apply equally to systemd `personad.socket` listeners and hub-created listeners.

**Preserve the source through every hop**: upstream headers contain the source it sees. If it is the public entry point, enable PROXY on its connection to hub (`proxy_protocol on;` in nginx `stream`). If another forwarding layer (tunnel client, another port forward) connects to it, it again sees loopback; that layer must also send PROXY, and the upstream must receive, trust, and pass it onward. nginx `stream` example (requires `ngx_stream_realip_module`, `--with-stream_realip_module` in `nginx -V`; example addresses / ports):

```nginx
stream {
    map $ssl_preread_server_name $backend {
        hub.example.com 127.0.0.1:18662;   # hub TLS port
        default         127.0.0.1:8444;
    }
    server {
        listen 127.0.0.1:8443 proxy_protocol;  # Previous hop also sends PROXY; omit proxy_protocol if nginx is the entry point
        set_real_ip_from 127.0.0.1;            # Trust only the previous hop; omit if nginx is the entry point
        ssl_preread on;
        proxy_pass $backend;
        proxy_protocol on;                     # Pass client address to hub
    }
}
```

See the previous layer's docs for enabling PROXY (often "proxy protocol" / "send-proxy", v1 / v2 options; hub accepts both). Other backends in `map` also receive headers; unsupported services need a separate `server` without `proxy_protocol on;`.

**Self-check after configuration**:

1. Restart hub; logs include `hub: PROXY protocol on the TLS listener, from 127.0.0.1/32`; `security.proxy_protocol` is "OK" in `personad hub doctor`.
2. Connect publicly on phone cellular. Authentication-failure / admin-login source logs should no longer be `127.0.0.0/24`. With `admin.access: cidrs`, public `/admin` opens the ordinary web app.
3. `proxy protocol: refused a connection from …` logs (at most one per minute, cumulative count) mean an upstream sent no header. Check `proxy_protocol on;` took effect, or other programs bypass the proxy to TLS.
4. `passed on a connection from loopback` (logged once) means source remains loopback: previous hop failed to pass it through. Fix the full chain above.

**Important**: once enabled, every connection from `from` to this port needs a header. Other local programs connecting directly to TLS (`https://127.0.0.1:<TLS-port>`) are rejected; use plaintext or a LAN address. In Docker with userspace docker-proxy port mapping, every mapped connection may appear from the gateway. Adding that gateway to `from` means this port is proxy-only.

<a id="反向代理的子路径android"></a>

### Reverse-proxy subpaths

Hub can live under a reverse-proxy subpath (`public_url: https://<hub-address>/<prefix>`, with the proxy stripping the prefix before forwarding to hub's plaintext port). Hub then has no TLS itself and no `fp` in `hub pair` links; the proxy uses a public-CA certificate. The web client verifies HTTPS through system trust. Web requires proxy `X-Forwarded-Prefix: /<prefix>`; hub adjusts `index.html` `<base>`, placing APIs, WebSocket, images, and routes under it ([web.md](../web/web.md), "Subpaths"; admin UI only at root).

- **Trade-off**: pinning trusts one public key; system trust accepts every system CA, any of which can impersonate hub by issuing for its domain (compromised / misissuing CA, device management installing system certificates). Same as browser access: better than plaintext, weaker than pinning. Prefer hub TLS + `fp` when available. Pairing codes appear only when scanned / pasted; hub identity still checked by `hub_id`.

Persona Link on computers (active mode) likewise accepts HTTPS pairing without `fp`: preserves prefix, verifies system CA / hostname; `fp` pairings remain pinned without fallback. Hub allows plaintext link requests only from same-machine or LAN proxies carrying `X-Forwarded-Proto: https`; see [link.md](link.md), "Hub behind a reverse proxy."

Public plaintext APIs default to `403 tls_required`, except `GET /api/v1/health`. This refusal happens after the request arrives and cannot retract an already-plaintext token; clients must choose the protocol before requesting.

## Behind Cloudflare Access

Use this when hub is published on a public hostname through Cloudflare Tunnel with Cloudflare Access in front, so clients reach it over the internet without a system VPN (#3502). Access answers requests without credentials with a `302` to its login page. Browsers (web app) handle the login themselves; a client that cannot (a script, an API client) sends an Access **service token** as the `CF-Access-Client-Id` and `CF-Access-Client-Secret` headers. No hub changes.

**Cloudflare dashboard**:

1. Zero Trust › Access › Service Auth › Service Tokens › create a token. Copy the **Client ID** and **Client Secret**; the secret is shown only once.
2. In hub's Access application, add a policy with Action **Service Auth** that includes that service token. Keep the existing login policy so browsers still work.
3. WebSocket must pass through the tunnel (`/api/v1/ws`). Cloudflare Tunnel allows it by default; don't turn it off.

**Warning: `cloudflared` on hub's machine makes internet traffic loopback.** The tunnel connects to hub from `127.0.0.1`, so hub treats every public request as local: the main token works, the admin UI skips `admin.access`, and authentication failures are not throttled ([PROXY protocol](#proxy-protocol) lists the effects). Hub never trusts `Cf-Connecting-IP` or other forwarded headers. Configure `security.proxy_protocol` and `security.trusted_cidrs` as described in [Source classification and reverse proxies](#source-classification-and-reverse-proxies); PROXY protocol helps only when the hop that connects to hub sends a PROXY header carrying the real client address. If yours can't, Access is the only thing in front of hub, so keep its policies tight.

<a id="网络排查"></a>

## Network troubleshooting

Run `personad hub doctor` on the hub machine first (it prints in the terminal's language; `PERSONA_LANG=zh-Hans` or `ja` picks another, and the admin page's **Run checks** shows the same report in the page's language). Read-only: checks hub.json plaintext `listen`, `public.tls_listen`, `security.allow_public_plaintext`, `security.plaintext_loopback_only`, TLS certificate, and that the data directory's credential files (hub.json, `secret.key`, `relay-tunnel.token`, the key and credential suffixes, `docker/db-password`…) are mode 0600 and its whole-secret folders (`tls/`, `docker/`, `extensions/data/`…) are 0700; asks running hub for candidates; lists each local IPv6 as publishable / excluded with reasons; on Linux checks public-IPv6 routes through TUN / virtual interfaces and ufw / nft rules; ends with next steps: which TLS port to allow, which URL to test on cellular. **No external reachability probes**: local access to its own address cannot prove public ingress.

TUN proxy automatic routes may send public-connection responses into the proxy, causing timeout or interrupted handshakes. **Not measured**. On a hub authorized for troubleshooting, inspect IPv6 routes to the phone source and capture on physical / TUN interfaces: did requests arrive, did replies leave correctly? Temporarily disable TUN for comparison. If implicated, preserve direct reply routing for inbound connections in proxy / policy routing; `trusted_cidrs` cannot fix return routes.

| Symptom | Check |
| --- | --- |
| Works on same network, cellular timeout | Phone IPv6; modem / router / host ingress rules; expired advertised addresses; TUN return route |
| `403 tls_required` | TLS port + HTTPS / WSS; never send tokens to public HTTP |
| `403 master_token_local_only` | Use temporary pairing or device tokens publicly |
| `429 rate_limited` | Wait `Retry-After`, stop repeated reconnects with invalid tokens |
| Public-key mismatch | Changed key, wrong hub, TLS-terminating proxy; re-pair through a trusted channel, never skip pinning |
| Public-key mismatch only away from home | A LAN address the hub advertised (`public_url` or `advertise_lan`) is another device on that network. To stop handing it out, set `public_url` to a name and `public.advertise_lan` to `false` |
| `hub pair` cannot connect locally | Hub running, HTTP supports IPv4 loopback, command configuration matches running instance |
| TLS inaccessible after enabling `security.proxy_protocol` | `proxy protocol: refused …` logs: upstream not sending headers, or direct bypass ([PROXY protocol](#proxy-protocol)) |
| A browser's Web Push test fails, or its last notification didn't go out (#7459) | The hub machine must reach the browsers' push services over https (`fcm.googleapis.com`, `*.push.services.mozilla.com`, `*.push.apple.com`, `*.notify.windows.com`); a firewall or proxy that blocks outbound https stops Web Push. `personad hub doctor` lists each browser and its last failure ([web.md](../web/web.md) "Web Push") |

<a id="命令行验证"></a>

## Command-line verification

For self-signed certificates, pin a trusted public key before calling APIs. curl `sha256//` uses standard base64, not QR base64url. Set `FP` to a fingerprint from trusted scanning / local `hub pair`, `TOKEN` to the device token, and run on your own external test machine:

```sh
PIN=$(printf '%s' "$FP" | tr '_-' '/+')=
curl --pinnedpubkey "sha256//$PIN" --insecure \
  -H "Authorization: Bearer $TOKEN" \
  https://hub.example.com:18662/api/v1/endpoints
```

Here `--insecure` skips system CA / hostname checks, while `--pinnedpubkey` still verifies the public key; use both together. Never obtain `FP` from the same unverified public request. IPv6 literal URLs require brackets, e.g. `https://[2001:db8::1234]:18662` (documentation address, not reachable). Response fields: [protocol](../protocol/protocol.md#候选地址).
