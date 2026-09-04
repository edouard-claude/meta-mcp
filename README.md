# metasocial-mcp

Serveur MCP distant et multi-tenant pour l'organique **Facebook** et **Instagram**.

Chaque utilisateur connecte son client MCP (Claude.ai, Claude Desktop, Claude Code,
ou tout client conforme), se logue avec son propre compte Facebook, et pilote
**ses** Pages et **ses** comptes Instagram professionnels : lecture des
statistiques, des publications et des commentaires, publication et réponse aux
commentaires.

- Transport : Streamable HTTP sur `POST /mcp`
- Authentification : serveur OAuth 2.1 intégré (DCR, PKCE S256, JWT HS256)
- Identité : fédérée à Facebook Login for Business
- Stockage : un fichier SQLite, jetons Meta chiffrés en AES-256-GCM
- Déploiement : un binaire statique, une image `distroless`, un volume

La publicité n'est pas couverte : pour les campagnes, utilisez le MCP officiel
`https://mcp.facebook.com/ads`.

---

## 1. Créer l'application Meta

Tout se passe sur [developers.facebook.com](https://developers.facebook.com/apps).

### 1.1 Créer l'app

1. **Créer une application** → cas d'usage **« Autre »** → type **Entreprise**.
2. Notez l'**identifiant** et la **clé secrète** dans *Paramètres → Général* :
   ce sont `META_APP_ID` et `META_APP_SECRET`.

### 1.2 Renseigner les URL

Dans *Paramètres → Général*, avec `PUBLIC_URL` = l'adresse publique du serveur :

| Champ | Valeur |
|---|---|
| URL de la politique de confidentialité | `PUBLIC_URL/privacy` |
| URL de suppression des données utilisateur | `PUBLIC_URL/meta/data-deletion` |

Ajoutez ensuite le produit **Connexion Facebook pour les entreprises**, puis dans
ses *Paramètres* :

| Champ | Valeur |
|---|---|
| URI de redirection OAuth valides | `PUBLIC_URL/meta/callback` |
| URL de rappel de désautorisation | `PUBLIC_URL/meta/deauthorize` |
| Connexion OAuth du client | Oui |
| Connexion OAuth web | Oui |

### 1.3 Autorisations demandées

Le serveur demande ces permissions au moment du login :

```
pages_show_list, pages_read_engagement, pages_manage_posts,
pages_read_user_content, pages_manage_engagement, read_insights,
instagram_basic, instagram_manage_insights, instagram_content_publish,
instagram_manage_comments, business_management
```

Elles se règlent avec `META_SCOPES` si vous voulez un périmètre plus étroit,
par exemple sans les permissions d'écriture.

### 1.4 Rester en mode développement

C'est le point important pour un usage entre proches : **en mode
développement, aucune vérification (App Review) n'est nécessaire**, mais seuls
les comptes déclarés dans l'application peuvent se connecter.

Dans *Rôles de l'app → Rôles*, ajoutez chaque personne comme **testeur** (elle
doit accepter l'invitation depuis ses notifications Facebook). Elles pourront
alors se loguer et accorder toutes les permissions ci-dessus.

En complément, `ALLOWED_META_USER_IDS` permet de restreindre encore côté
serveur : seuls les identifiants Facebook listés peuvent créer un tenant.

### 1.5 Côté utilisateur

Pour qu'Instagram remonte, le compte doit être **professionnel** (Business ou
Créateur) et **lié à une Page Facebook** que l'utilisateur administre. Un
compte personnel n'expose aucune statistique.

---

## 2. Configuration

| Variable | Obligatoire | Défaut | Description |
|---|---|---|---|
| `PUBLIC_URL` | oui | | Adresse publique, en `https://`, sans slash final |
| `LISTEN_ADDR` | non | `:8080` | Adresse d'écoute interne (HTTP) |
| `DB_PATH` | non | `/data/metasocial.db` | Fichier SQLite |
| `TOKEN_CIPHER_KEY` | oui | | 32 octets en base64, clé AES-256-GCM |
| `JWT_SIGNING_KEY` | oui | | >= 32 octets en base64, HMAC-SHA256 |
| `META_APP_ID` | oui | | Identifiant de l'app Meta |
| `META_APP_SECRET` | oui | | Clé secrète de l'app Meta |
| `META_GRAPH_VERSION` | non | `v26.0` | Version de la Graph API |
| `META_SCOPES` | non | voir §1.3 | Permissions demandées, séparées par des virgules |
| `ACCESS_TOKEN_TTL` | non | `1h` | Durée de vie des jetons d'accès MCP |
| `REFRESH_TOKEN_TTL` | non | `720h` | Durée de vie des refresh tokens |
| `LOG_FORMAT` | non | `json` | `json` en production, `text` en local |
| `ALLOWED_META_USER_IDS` | non | | Liste CSV d'identifiants Facebook autorisés |

Le binaire **refuse de démarrer** si une variable obligatoire manque, si une
clé n'a pas la bonne taille, ou si `PUBLIC_URL` n'est pas en `https://`.

Générer les clés :

```bash
openssl rand -base64 32   # TOKEN_CIPHER_KEY
openssl rand -base64 32   # JWT_SIGNING_KEY
```

Deux variables supplémentaires, `META_GRAPH_BASE_URL` et `META_DIALOG_BASE_URL`,
détournent le client Graph vers un autre serveur. Elles n'existent que pour le
test de bout en bout et n'ont aucune raison d'être définies en production.

---

## 3. Déploiement sur CapRover

Le dépôt contient déjà un `Dockerfile` multi-étapes (build `golang:1.26-alpine`,
image finale `distroless/static`) et le `captain-definition` qui pointe dessus.

### 3.1 Créer l'app

1. Dashboard CapRover → **Apps** → créer `metasocial-mcp`, avec
   *Has Persistent Data* coché.
2. Onglet **App Configs** :
   - *Persistent Directories* : chemin dans le conteneur `/data`, label du
     volume `metasocial-data`. Sans ce volume, la base est perdue à chaque
     déploiement et tout le monde doit se reconnecter.
   - *Environmental Variables* : les variables du §2.
   - *Container HTTP Port* : `8080`.
3. Onglet **HTTP Settings** : activer HTTPS et **Force HTTPS**. Le binaire
   n'écoute qu'en HTTP en interne ; c'est CapRover qui termine le TLS.
4. Onglet **Deployment** → *Enable App Token*, et notez le jeton.

### 3.2 Déployer

Le CLI `caprover` est cassé sur Node >= 26 ; l'API fait la même chose :

```bash
git archive --format=tar -o app.tar HEAD

curl -sS -X POST \
  -H "x-captain-app-token: $APP_TOKEN" \
  -H "x-namespace: captain" \
  -F "sourceFile=@app.tar" \
  "https://captain.<domaine>/api/v2/user/apps/appData/metasocial-mcp?detached=1"
```

Deux pièges :

- l'en-tête est `x-captain-app-token` pour un **app token**. Un app token
  envoyé dans `x-captain-auth` répond `{"status":1106,"description":"Auth token
  corrupted"}`, ce qui ressemble à un jeton mal copié mais n'est qu'un mauvais
  en-tête ;
- CapRover répond **HTTP 200 même quand il refuse**. Le verdict est dans le
  champ `status` du JSON : `100` et `101` valent succès, tout le reste est un
  échec.

### 3.3 Vérifier

```bash
curl -s https://<domaine>/healthz                     # {"status":"ok"}
curl -s https://<domaine>/.well-known/oauth-protected-resource
```

Et depuis le serveur, pour confirmer que la nouvelle image tourne vraiment :

```bash
sudo docker service ps srv-captain--metasocial-mcp --no-trunc \
  --format '{{.Image}} | {{.CurrentState}}' | head -3
```

Un `img-captain-metasocial-mcp:N | Running` au-dessus du `:N-1 | Shutdown` est
la signature d'un déploiement qui vient de passer.

---

## 4. Connecter un nouvel utilisateur

Une fois l'app Meta configurée et la personne ajoutée comme testeuse :

1. Dans **Claude.ai** : *Paramètres → Connecteurs → Ajouter un connecteur
   personnalisé*, coller `https://<domaine>/mcp`.
   Dans **Claude Desktop** ou **Claude Code**, ajouter le même serveur distant.
2. Le client découvre tout seul le serveur d'autorisation, s'enregistre
   (DCR) et ouvre une page **« Connecter votre compte Facebook »**.
3. L'utilisateur clique, se logue chez Facebook, **coche les Pages** qu'il veut
   partager et valide les permissions.
4. Le navigateur revient vers le client MCP, qui obtient son jeton. C'est fini.

Vérification : demander « liste mes pages » doit appeler `list_pages` et
renvoyer les Pages du compte.

Si un outil répond « autorisation expirée, utilisez reconnect_url », appeler
`reconnect_url` donne un lien à ouvrir pour réautoriser l'accès. C'est ce qui
arrive quand l'utilisateur change son mot de passe Facebook, révoque l'app, ou
au bout de 60 jours d'inactivité.

Pour tout supprimer, l'utilisateur retire l'application depuis ses paramètres
Facebook : Meta appelle `/meta/data-deletion`, et le tenant, ses pages et ses
sessions disparaissent.

---

## 5. Les outils

### Lecture

| Outil | Paramètres | Retour |
|---|---|---|
| `list_pages` | — | Pages connues, sans appel à Meta |
| `sync_pages` | — | Relit les pages depuis Meta et remplace la liste |
| `page_insights` | `page_id`, `since?`, `until?`, `metrics?` | Séries de métriques quotidiennes |
| `page_insights_metadata` | `page_id` | Métriques disponibles sur cette page |
| `page_posts` | `page_id`, `since?`, `limit?` | Publications avec impressions, clics, réactions |
| `page_post_comments` | `post_id`, `page_id?`, `limit?` | Commentaires d'une publication |
| `ig_account_insights` | `page_id`, `since?`, `until?`, `metrics?` | Statistiques du compte Instagram |
| `ig_follower_demographics` | `page_id`, `breakdown` | Répartition des abonnés |
| `ig_media` | `page_id`, `since?`, `limit?` | Publications Instagram et leurs statistiques |
| `ig_media_comments` | `media_id`, `page_id?`, `limit?` | Commentaires d'une publication Instagram |
| `reconnect_url` | — | Lien à usage unique pour réautoriser Facebook |

### Écriture

| Outil | Paramètres |
|---|---|
| `page_publish_post` | `page_id`, `message?`, `link?`, `photo_url?`, `scheduled_at?`, `confirm` |
| `page_reply_comment` | `comment_id`, `message`, `page_id?`, `confirm` |
| `ig_publish` | `page_id`, `media_type`, `image_url?`/`video_url?`/`children?`, `caption?`, `confirm` |
| `ig_reply_comment` | `comment_id`, `message`, `page_id?`, `confirm` |

**Sans `confirm=true`, ces outils renvoient un aperçu et n'appellent jamais
l'API en écriture.** Le modèle doit montrer l'aperçu, obtenir l'accord de
l'utilisateur, puis rappeler l'outil avec `confirm=true`.

Les dates `since`/`until` sont au format `AAAA-MM-JJ` et couvrent les 28
derniers jours par défaut. `scheduled_at` est une date ISO 8601, entre 10
minutes et 6 mois dans le futur.

### Le paramètre `page_id` facultatif

Les identifiants Meta ne disent pas de quelle page ils dépendent. Pour les
outils qui prennent un `post_id`, un `comment_id` ou un `media_id`, la page est
déduite dans cet ordre : le `page_id` fourni, le préfixe `{page_id}_…` des
identifiants Facebook, puis la seule page (ou le seul compte Instagram)
connecté. Si plusieurs pages sont connectées et qu'aucune piste ne tranche,
l'outil demande explicitement `page_id` plutôt que de deviner.

---

## 6. Isolation entre utilisateurs

C'est la propriété centrale du serveur, et elle est vérifiée par des tests.

- Le `tenant_id` vient toujours du JWT vérifié, jamais d'un paramètre d'outil.
- Le store n'expose aucune méthode qui accepte un `page_id` sans `tenant_id` :
  il n'existe pas de chemin de code capable de résoudre une page hors tenant.
- Un `page_id` appartenant à un autre tenant est signalé « page inconnue pour
  ce compte », exactement comme un identifiant inexistant : rien ne permet de
  distinguer les deux, donc rien ne permet de sonder les autres comptes.
- Aucun jeton Meta n'apparaît dans une réponse d'outil : les structures
  renvoyées ne les sérialisent pas.
- Les journaux ne contiennent jamais de jeton, de code OAuth ni de query
  string.

---

## 7. Développement

```bash
make build     # binaire statique dans bin/
make test      # tests unitaires et d'intégration
make check     # gofmt + go vet + staticcheck + tests, la barrière de commit
make e2e       # lance le binaire contre un faux Graph, puis l'inspecteur MCP
make run       # démarre le binaire avec l'environnement courant
```

Le test `make e2e` exécute `npx @modelcontextprotocol/inspector --cli` : le
premier lancement télécharge le paquet et peut prendre plusieurs minutes. Sans
`npx`, cette partie est ignorée.

### Architecture

```
cmd/metasocial-mcp/     composition root, flags/env, arrêt propre
internal/domain/        entités et ports, zéro dépendance
internal/app/           cas d'usage, un fichier par outil MCP
internal/adapters/
  sqlite/               TenantStore
  crypto/               TokenCipher AES-256-GCM
  meta/                 client Graph API et OAuth Meta
  authserver/           serveur OAuth 2.1 (DCR, PKCE, JWT)
  mcpserver/            enregistrement des outils, vérification du Bearer
  httpserver/           routeur net/http
  clock/                horloge système
internal/config/        lecture et validation de l'environnement
internal/e2e/           test de bout en bout du binaire
migrations/             SQL embarqué, appliqué au démarrage
web/                    pages HTML embarquées
```

Règle de dépendance : `cmd` → `adapters` → `app` → `domain`, jamais l'inverse.
Les adapters ne s'importent pas entre eux ; ils communiquent par les ports
déclarés dans `domain`, ou par des interfaces déclarées au point d'usage.

### Endpoints HTTP

| Méthode | Chemin | Rôle |
|---|---|---|
| `GET` | `/healthz` | Sonde de santé, vérifie l'ouverture de SQLite |
| `GET` | `/.well-known/oauth-protected-resource` | Métadonnées RFC 9728 |
| `GET` | `/.well-known/oauth-authorization-server` | Métadonnées RFC 8414 |
| `POST` | `/oauth/register` | Enregistrement dynamique de client (RFC 7591) |
| `GET` | `/oauth/authorize` | Demande d'autorisation, PKCE S256 obligatoire |
| `POST` | `/oauth/token` | `authorization_code` et `refresh_token` |
| `GET` | `/meta/login` | Page « Connecter votre compte Facebook » |
| `GET` | `/meta/callback` | Retour du dialogue Facebook |
| `POST` | `/meta/data-deletion` | Callback signé de suppression des données |
| `GET` | `/meta/deauthorize` | Confirmation de suppression |
| `GET` | `/privacy` | Politique de confidentialité |
| `POST` | `/mcp` | Endpoint MCP, Bearer obligatoire |
