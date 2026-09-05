<div align="center">

# meta-mcp

**Your Facebook Pages and Instagram accounts, one prompt away.**

A single Go binary that puts the organic side of the [Meta Graph API](https://developers.facebook.com/docs/graph-api)
in front of any MCP client, for you and for the people you invite.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-Streamable%20HTTP-8A63D2)](https://modelcontextprotocol.io)
[![OAuth](https://img.shields.io/badge/OAuth-2.1%20%2B%20PKCE-important)](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1)
[![Graph API](https://img.shields.io/badge/Graph%20API-v26.0-1877f2?logo=meta&logoColor=white)](https://developers.facebook.com/docs/graph-api/changelog)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Stars](https://img.shields.io/github/stars/edouard-claude/meta-mcp?style=flat&logo=github&color=f5c518)](https://github.com/edouard-claude/meta-mcp/stargazers)

</div>

---

Ask your assistant *"how did the bakery page do last month?"*, *"which Instagram post
got the most saves?"*, *"reply to the last three comments"* and get an answer from the
real data, then publish back to it.

```console
> Compare la portée de mes deux pages sur août, et sors le top 3 des posts Instagram.

  list_pages           -> 2 pages
  page_insights        -> page_impressions_unique, 31 points
  ig_media             -> 24 médias, triés par reach

  Boulangerie du Port  48 210 personnes atteintes  (+12 % vs juillet)
  Snack Créole         11 043 personnes atteintes  (-4 %)
  ...
```

This is a **remote, multi-tenant** MCP server. You host it once; each person connects
their own Facebook account and only ever sees their own pages.

## Why

Meta's official MCP server covers **ads**. This one covers everything else: the
organic performance of Pages and Instagram Business accounts, the posts, the
comments, and publishing.

- **Multi-tenant by construction.** The tenant comes from the verified JWT, never
  from a tool parameter, and the store has no method that resolves a `page_id`
  without a `tenant_id`. Isolation is structural, not defensive.
- **Its own OAuth 2.1 server.** Dynamic client registration, mandatory PKCE S256,
  rotating refresh tokens, HS256 access tokens. Any conforming MCP client connects
  itself, with no manual token handling.
- **Fail-closed writes.** Nothing is published, hidden or deleted without
  `confirm=true`. Without it, the tool returns a preview and never calls the API.
- **Sessions that do not rot.** Meta user tokens are renewed before they expire, so
  a connected account keeps working past the 60 day mark on its own.
- **One static binary.** `CGO_ENABLED=0`, a `distroless` image, a SQLite file on a
  volume. Two dependencies in `go.mod`, both justified there.

## How it works

Two OAuth layers stacked: the server is an identity provider for the MCP client,
and an OAuth client towards Facebook.

```
Claude.ai                 meta-mcp                        Facebook
   |                         |                                |
   |--- POST /mcp ---------->|                                |
   |<-- 401 WWW-Authenticate: resource_metadata=...           |
   |--- GET /.well-known/oauth-protected-resource ----------->|
   |--- POST /oauth/register (DCR) -> client_id               |
   |--- GET /oauth/authorize (PKCE S256) -------------------->|
   |          parks the request in login_states (TTL 10 min)  |
   |<-- 302 /meta/login?state=...                             |
   |                         |                                |
   |  (browser)              |--- 302 dialog/oauth ---------->|
   |                         |<-- GET /meta/callback?code ----|
   |                         |    code -> short-lived token   |
   |                         |    -> long-lived token (60 d)  |
   |                         |    GET /me, GET /me/accounts   |
   |                         |    upsert tenant + pages, AES-256-GCM
   |<-- 302 redirect_uri?code=... & original state            |
   |--- POST /oauth/token (code_verifier) ------------------->|
   |<-- JWT HS256: sub=tenant_id, aud=PUBLIC_URL/mcp          |
   |                         |                                |
   |--- POST /mcp + Bearer --|                                |
   |     JWT verified -> tenant_id                            |
   |     page_id checked against that tenant's pages          |
   |                         |--- Graph API (page token) ---->|
```

Sessions are stateless: restarting the container disconnects nobody, as long as
the JWTs have not expired.

## Set up the Meta app

Everything happens on [developers.facebook.com](https://developers.facebook.com/apps).

**1. Create the app.** *Create an app*, then under *Content management* tick both
**Manage everything on your Page** and **Manage messages and content on Instagram**.
Those two use cases pre-wire the permission set below; the generic *Other* path leaves
you adding every permission by hand. Skip the business portfolio unless you plan to
publish the app. Copy the app id and secret from *Settings, Basic*: those are
`META_APP_ID` and `META_APP_SECRET`.

Note that Meta rejects any app name containing `meta`, `fb`, `face`, `book`, `insta`
or `gram`: pick something else, the name is only what users see on the login dialog.

**2. Fill in the URLs**, with `PUBLIC_URL` the public address of your server:

| Where | Field | Value |
|---|---|---|
| Settings, Basic | Privacy Policy URL | `PUBLIC_URL/privacy` |
| Settings, Basic | User data deletion | `PUBLIC_URL/meta/data-deletion` |
| Facebook Login for Business, Settings | Valid OAuth Redirect URIs | `PUBLIC_URL/meta/callback` |
| Facebook Login for Business, Settings | Deauthorize callback URL | `PUBLIC_URL/meta/deauthorize` |

**3. Stay in development mode.** This is the point for a server shared between
friends: in development mode **no App Review is needed**, but only accounts declared
in the app can log in. Add each person as a **tester** in *App Roles*; they accept
from their Facebook notifications. `ALLOWED_META_USER_IDS` narrows it further,
server-side.

**4. Tell your users** their Instagram account must be **Business or Creator** and
**linked to a Page** they administer. A personal account exposes no insights.

**5. Check the permissions.** In *Use cases*, customize each one and make sure every
scope below reads **Ready for testing**. The two use cases do not grant all of them on
their own, so a few need adding by hand:

```
pages_show_list, pages_read_engagement, pages_manage_posts,
pages_read_user_content, pages_manage_engagement, read_insights,
instagram_basic, instagram_manage_insights, instagram_content_publish,
instagram_manage_comments, business_management
```

One trap: the Instagram use case opens on *API setup with Instagram login*, whose
`instagram_business_*` scopes belong to a different token model. This server uses
Facebook Login, so it needs the `instagram_basic` family listed above. Adding them
from the *Permissions and features* tab is enough; you can ignore the Instagram login
setup screen entirely.

Set `META_SCOPES` to narrow the list down, for instance to drop the write permissions.

## Configure

| Variable | Required | Default | What it is |
|---|---|---|---|
| `PUBLIC_URL` | yes | | Public address, `https://`, no trailing slash |
| `LISTEN_ADDR` | no | `:8080` | Internal listen address (plain HTTP) |
| `DB_PATH` | no | `/data/metasocial.db` | SQLite file |
| `TOKEN_CIPHER_KEY` | yes | | 32 bytes, base64, AES-256-GCM key |
| `JWT_SIGNING_KEY` | yes | | >= 32 bytes, base64, HMAC-SHA256 key |
| `META_APP_ID` | yes | | Meta app id |
| `META_APP_SECRET` | yes | | Meta app secret |
| `META_GRAPH_VERSION` | no | `v26.0` | Graph API version |
| `META_SCOPES` | no | see above | Requested permissions, comma separated |
| `ACCESS_TOKEN_TTL` | no | `1h` | MCP access token lifetime |
| `REFRESH_TOKEN_TTL` | no | `720h` | Refresh token lifetime |
| `LOG_FORMAT` | no | `json` | `json` in production, `text` locally |
| `ALLOWED_META_USER_IDS` | no | | CSV allow-list of Facebook user ids |

```bash
openssl rand -base64 32   # TOKEN_CIPHER_KEY
openssl rand -base64 32   # JWT_SIGNING_KEY
```

The binary refuses to start on a missing variable, a key of the wrong size, or a
`PUBLIC_URL` that is not `https://`.

## Deploy

<details open>
<summary><b>CapRover</b></summary>

The repository ships the `Dockerfile` (multi-stage, `golang:1.26-alpine` to
`distroless/static`) and the `captain-definition` that points at it.

1. Create the app with **Has Persistent Data** checked.
2. *App Configs*: persistent directory `/data`, the environment variables above,
   container HTTP port `8080`. Without the volume the database is wiped on every
   deploy and everyone has to reconnect.

   The image runs unprivileged (uid 65532) and ships `/data` owned by that uid, so a
   volume created from it is writable. A volume that already exists from an earlier
   deploy stays `root`-owned and the process dies on `unable to open database file`:
   delete it and let CapRover recreate it, or fix it once with
   `sudo docker run --rm -v <volume>:/data busybox chown -R 65532:65532 /data`.
3. *HTTP Settings*: enable HTTPS and **Force HTTPS**. The binary only speaks plain
   HTTP internally; CapRover terminates TLS.
4. *Deployment*: **Enable App Token**, keep the token.

```bash
git archive --format=tar -o app.tar HEAD

curl -sS -X POST \
  -H "x-captain-app-token: $APP_TOKEN" \
  -H "x-namespace: captain" \
  -F "sourceFile=@app.tar" \
  "https://captain.<domain>/api/v2/user/apps/appData/meta-mcp?detached=1"
```

Two traps. The header is `x-captain-app-token` for an **app token**; the same token
sent as `x-captain-auth` answers `{"status":1106,"description":"Auth token corrupted"}`,
which looks like a bad paste but is only the wrong header. And CapRover answers
**HTTP 200 even when it refuses**: the verdict is the JSON `status` field, where
`100` and `101` mean success and everything else is a failure.

</details>

<details>
<summary><b>Anywhere else</b></summary>

```bash
docker build -t meta-mcp .
docker run -d --name meta-mcp \
  -p 8080:8080 -v meta-mcp-data:/data \
  -e PUBLIC_URL=https://mcp.example.com \
  -e TOKEN_CIPHER_KEY=... -e JWT_SIGNING_KEY=... \
  -e META_APP_ID=... -e META_APP_SECRET=... \
  meta-mcp
```

Put it behind a reverse proxy that terminates TLS on `PUBLIC_URL`. Or build it
straight: `make build`, then run `bin/metasocial-mcp`.

</details>

Check it:

```console
$ curl -s https://<domain>/healthz
{"status":"ok"}

$ curl -s https://<domain>/.well-known/oauth-protected-resource
{"resource":"https://<domain>/mcp","authorization_servers":["https://<domain>"],"bearer_methods_supported":["header"]}
```

## Connect a client

There is nothing to configure per user beyond the URL: the client registers itself
and runs the OAuth flow on its own.

<details open>
<summary><b>Claude.ai</b></summary>

*Settings, Connectors, Add custom connector*, paste `https://<domain>/mcp`.
</details>

<details>
<summary><b>Claude Code</b></summary>

```bash
claude mcp add --scope user --transport http meta https://<domain>/mcp
```
</details>

<details>
<summary><b>Claude Desktop</b></summary>

*Settings, Connectors, Add custom connector*, paste `https://<domain>/mcp`. Custom
connectors need a paid plan.

Without them, bridge the remote server through stdio: `claude_desktop_config.json`
only knows how to spawn a local executable, it does not speak HTTP.

```json
{
  "mcpServers": {
    "meta": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://<domain>/mcp"]
    }
  }
}
```
</details>

<details>
<summary><b>Any other MCP client</b></summary>

Point it at `https://<domain>/mcp` over Streamable HTTP. The server advertises its
authorization server in the `WWW-Authenticate` header of the 401, so a client that
implements the MCP authorization spec needs nothing else.
</details>

The browser goes straight to the Facebook dialog: pick the Pages to share, accept
the permissions, and it returns to the client, which gets its token. Done.

Ask *"list my pages"*: `list_pages` should answer with the Pages of that account.

## Tools

### Read

| Tool | What you get |
|---|---|
| `list_pages` | Known Pages and their linked Instagram account, straight from the store |
| `sync_pages` | Re-reads the Pages from Meta and replaces the list |
| `page_insights` | Daily organic metrics of a Page over a window |
| `page_insights_metadata` | Catalogue of metric names the server knows how to request |
| `page_posts` | Recent posts with clicks, reactions and video views |
| `page_post_comments` | Comments on a post |
| `ig_account_insights` | Reach, views, profile views, engaged accounts, interactions, followers |
| `ig_follower_demographics` | Follower breakdown by `city`, `country`, `age` or `gender` |
| `ig_media` | Recent Instagram media with reach, views, saves, shares, interactions |
| `ig_media_comments` | Comments on an Instagram media |
| `page_post_insights` | Full metrics of one Page post, beyond the three page_posts flattens |
| `ig_media_insights` | Full metrics of one Instagram media |
| `ig_stories` | Stories currently live, which Meta drops after 24 hours |
| `page_scheduled_posts` | Posts Meta is holding for a future date |
| `connection_status` | Token validity, expiry, granted permissions, synced pages |
| `reconnect_url` | Single-use link to re-authorize Facebook |

`since` and `until` are `YYYY-MM-DD`. The insights tools default to the last 28 days;
`page_posts` and `ig_media` apply no lower bound at all when `since` is absent, so a
page that publishes twice a month does not come back empty.

Meta has deprecated the entire post impressions family (`post_impressions`,
`post_impressions_unique`, `post_impressions_organic`, `post_engaged_users`,
`post_activity`), so `page_posts` reports clicks, reactions and video views and no
impressions at all rather than a permanent zero dressed up as data.

Meta rejects a whole insights batch when one metric name is unsupported on that
object. Rather than failing, the server retries metric by metric and returns what it
could read, listing the rest under `rejected`. `page_insights_metadata` is a
maintained catalogue in the code, not a live capability check: the
`/insights/metadata` endpoint no longer answers, so `rejected` is the real signal.

### Write

| Tool | What it does |
|---|---|
| `page_publish_post` | Publishes a message, a link or a photo, now or scheduled |
| `page_reply_comment` | Replies to a comment on a Page post |
| `ig_publish` | Publishes an image, a reel, a carousel or a story |
| `ig_reply_comment` | Replies to a comment on an Instagram media |
| `page_moderate_comment` | Hides, unhides or deletes a comment on a Page post |
| `ig_moderate_comment` | Same on an Instagram media |
| `page_cancel_scheduled_post` | Deletes a post Meta had not published yet |
| `page_delete_post` | Deletes a published post, with its reactions and comments |

The last four are annotated as destructive, so a client can warn before running
them, and their preview says plainly what cannot be undone.

Scheduling goes through Meta's own `scheduled_publish_time`: `scheduled_at` is an
ISO 8601 date, between 10 minutes and 6 months out, and `page_scheduled_posts` plus
`page_cancel_scheduled_post` are how you see and undo it afterwards. Instagram
publishing is the two-step container flow, with the server waiting for Meta to
finish processing.

Meta ids do not say which Page they belong to. For tools taking a `post_id`,
`comment_id` or `media_id`, the Page is resolved in that order: the `page_id` you
passed, the `{page_id}_...` prefix Facebook uses, then the only connected Page. If
several Pages are connected and nothing settles it, the tool asks for `page_id`
rather than guessing across accounts.

### Resources and prompts

Two resources expose the same data as tools, for clients that prefer to attach
context rather than call: `metasocial://pages` and `metasocial://connection`. Both
are tenant-scoped like everything else, so the same URI gives each user their own
data.

Two prompts drive the common workflows: `bilan_mensuel` builds a monthly organic
report for a Page and its Instagram account, and `revue_commentaires` walks through
recent comments, proposing replies and flagging what deserves moderation. The
reporting prompt forbids any write; the moderation one never suggests deleting on
its own, since hiding is reversible and deleting is not.

## The connection renews itself

A Meta user token lasts about 60 days. The server records when each one expires and
renews it twice a day for anyone inside a two week window, which is why a tenant who
keeps using the server never has to log in again. A token Meta refuses to renew, a
revoked app for instance, is reported in the logs and left alone: nothing is deleted
on the user's behalf, and `connection_status` tells them what happened with a link to
fix it.

## Nothing gets published without a human

Every write tool returns a preview until it is called again with `confirm=true`.

```json
{
  "preview": true,
  "page_id": "1234567890",
  "page_name": "Boulangerie du Port",
  "kind": "photo",
  "message": "Nouvelle fournée à 16h",
  "photo_url": "https://cdn.example.com/four.jpg",
  "notice": "Aperçu uniquement, rien n'a été publié. Montrez ce contenu à
             l'utilisateur et attendez son accord explicite, puis rappelez le
             même outil avec confirm=true."
}
```

The preview costs zero Graph calls: the API is not touched at all. Input is
validated first too, so a malformed URL or an impossible schedule fails before
anything leaves the process.

## Isolation

The property the whole design exists for, and the one the tests check hardest.

- The `tenant_id` always comes from the verified JWT, never from a tool parameter.
- The store exposes no method taking a `page_id` without a `tenant_id`: no code path
  can resolve a Page outside its tenant.
- A `page_id` owned by someone else reads back as *"page inconnue pour ce compte"*,
  exactly like an id that never existed. Nothing distinguishes the two, so nothing
  lets you probe other accounts.
- No Meta token is ever serialized into a tool response.
- Logs never carry a token, an OAuth code, or a query string.

A user who removes the app from their Facebook settings triggers the signed
`/meta/data-deletion` callback: tenant, Pages and sessions are gone immediately.

## Develop

```bash
make build     # static binary in bin/
make test      # unit and integration tests
make check     # gofmt + go vet + staticcheck + tests, the commit gate
make e2e       # runs the binary against a fake Graph, then the MCP inspector
```

Coverage: 100 % on `domain`, 91 % on `app`, 75 to 100 % on the adapters. `make e2e`
shells out to `npx @modelcontextprotocol/inspector --cli`; the first run downloads
it and can take minutes, and the step is skipped when `npx` is missing.

```
cmd/metasocial-mcp/     composition root, env, graceful shutdown
internal/
├── domain/             entities and ports, zero dependencies
├── app/                use cases, one file per MCP tool
├── adapters/
│   ├── sqlite/         TenantStore
│   ├── crypto/         TokenCipher, AES-256-GCM
│   ├── meta/           Graph API client and Meta OAuth
│   ├── authserver/     OAuth 2.1 server (DCR, PKCE, JWT)
│   ├── mcpserver/      tool registration, bearer verification
│   ├── httpserver/     net/http router
│   └── clock/          system clock
├── config/             environment reading and validation
└── e2e/                end-to-end test of the binary
migrations/             embedded SQL, applied at startup
web/                    embedded HTML pages
```

Dependency rule: `cmd` -> `adapters` -> `app` -> `domain`, never the other way.
Adapters do not import each other; they talk through the ports declared in `domain`,
or interfaces declared at the point of use.

### Endpoints

| Method | Path | Role |
|---|---|---|
| `GET` | `/healthz` | Health probe, opens SQLite |
| `GET` | `/.well-known/oauth-protected-resource` | RFC 9728 metadata |
| `GET` | `/.well-known/oauth-authorization-server` | RFC 8414 metadata |
| `POST` | `/oauth/register` | Dynamic client registration, RFC 7591 |
| `GET` | `/oauth/authorize` | Authorization request, PKCE S256 mandatory |
| `POST` | `/oauth/token` | `authorization_code` and `refresh_token` |
| `GET` | `/meta/login` | The "Continue with Facebook" page |
| `GET` | `/meta/callback` | Return from the Facebook dialog |
| `POST` | `/meta/data-deletion` | Signed data deletion callback |
| `GET` | `/meta/deauthorize` | Deletion confirmation page |
| `GET` | `/privacy` | Privacy policy |
| `POST` | `/mcp` | MCP endpoint, bearer required |

Ads are out of scope on purpose: Meta's own `https://mcp.facebook.com/ads` covers
campaigns. Not affiliated with Meta.

## License

MIT, see [LICENSE](LICENSE).
