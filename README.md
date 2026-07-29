# GoLedMatrix2

Serveur d’affichage léger pour matrices LED RGB HUB75 pilotées par Raspberry Pi,
et client Go chargé de préparer puis d’envoyer les images.

Le projet s’appuie sur
[`rpi-rgb-led-matrix`](https://github.com/hzeller/rpi-rgb-led-matrix) et reprend
l’API du wrapper
[`go-rpi-rgb-led-matrix`](https://github.com/zaggash/go-rpi-rgb-led-matrix).
La version utilisée est épinglée à
`v0.0.0-20231128121715-f3ceee87d19f`.

## État du projet

Fondations disponibles :

- serveur HTTP versionné et client en ligne de commande ;
- protocole RGB24 brut, sans décodage ni composition sur le Raspberry Pi ;
- file à une seule trame logique : si plusieurs images arrivent pendant un
  rendu, seule la plus récente est conservée ;
- backend matériel Linux/Raspberry Pi isolé derrière les build tags
  `linux,cgo,rpi` ;
- backend mémoire portable pour macOS, Linux, le développement et les tests ;
- simulateur graphique représentant chaque pixel de la matrice logique ;
- écran technique temporaire au démarrage, rendu avec une police bitmap ;
- trois horloges bitmap (`simple`, `fancy`, `round`) sélectionnables côté
  serveur ou client et affichées par défaut lorsqu’aucune trame client n’est
  active ;
- configuration TOML stricte du matériel, du runtime GPIO et du serveur HTTP ;
- arrêt propre, limites de taille, timeouts HTTP et tests avec race detector ;
- dépendances Go et bibliothèque C++ épinglées.

À venir :

- authentification et TLS pour une exposition hors réseau local ;
- bibliothèque de composition et ordonnanceur d’animations côté client ;
- métriques et mesures de débit/latence sur le matériel cible ;
- paquet Debian et procédure de désinstallation.

## Architecture

Le client compose une image à la géométrie exacte de la dalle, la convertit en
RGB24 puis l’envoie. Le serveur valide la taille, remplace la trame en attente et
effectue un `Render()` sur le prochain VSync.

`rpi-rgb-led-matrix` conserve ensuite cette image dans son buffer natif et
rafraîchit continuellement les GPIO. Il n’existe volontairement **aucune boucle
Go qui redessine une image inchangée** : elle consommerait du CPU et introduirait
de la gigue sans améliorer la stabilité.

```text
composition/PNG (client) -> RGB24 -> HTTP -> dernière trame -> Render/VSync
                                                           -> scan GPIO natif
```

## Protocole HTTP v1

Le protocole est volontairement minimal et orienté réseau local.

### `GET /healthz`

Retourne `200 OK` lorsque le processus HTTP répond.

### `GET /v1/info`

Décrit le contrat attendu par le serveur :

```json
{
  "protocol_version": "1",
  "width": 64,
  "height": 32,
  "pixel_format": "rgb24",
  "frame_bytes": 6144,
  "backend": "memory",
  "base_urls": ["http://192.168.0.18:8080"],
  "started_at": "2026-07-28T10:00:00Z",
  "uptime_seconds": 42,
  "stats": {"accepted": 0, "rendered": 0, "failed": 0}
}
```

`base_urls` fournit au client les adresses détectées ou configurées pour joindre
le serveur.

### `PUT /v1/frame`

- `Content-Type: application/vnd.goledmatrix.rgb24`
- corps de `width × height × 3` octets exactement ;
- pixels ordonnés par lignes, depuis le coin supérieur gauche ;
- trois octets par pixel : rouge, vert, bleu ;
- réponse `202 Accepted` avec `{"sequence": N}`.

`202` signifie que la trame a été validée et mise en attente. Elle peut être
remplacée par une trame plus récente avant le rendu si le producteur dépasse la
capacité du matériel. Ce choix évite d’accumuler une animation déjà périmée.
La première trame client suspend l’horloge par défaut.

### `POST /v1/display-info`

Demande au serveur de réafficher temporairement ses informations techniques sur
la matrice. La réponse est `202 Accepted`. Pendant cet écran, les nouvelles
trames client restent acceptées ; la plus récente est affichée automatiquement
à la fin.

Commande équivalente avec le client :

```bash
go run ./cmd/ledmatrix-client \
  -server http://192.168.0.18:8080 \
  -show-info
```

### `POST /v1/clock?mode={simple|fancy|round}`

Sélectionne la variante, réactive l’horloge du serveur et abandonne la dernière
trame client. La réponse est `202 Accepted` avec `{"mode":"round"}`. Les trois
variantes sont :

- `simple` : affichage horizontal `HH:MM`, séparateur clignotant et barre des
  secondes ;
- `fancy` : heures et minutes superposées avec deux couleurs ;
- `round` : cadran à douze repères, progression circulaire des secondes et
  heure centrée.

Tous les glyphes sont bitmap et leur agrandissement utilise uniquement un
facteur entier.

Commande équivalente avec le client :

```bash
go run ./cmd/ledmatrix-client \
  -server http://192.168.0.18:8080 \
  -clock round
```

Les erreurs utilisent `application/problem+json`. La version 1 n’inclut ni
compression, ni redimensionnement, ni delta entre images : ces opérations
coûteuses restent du côté client.

## Configuration du serveur

Le fichier complet de référence est
[`config.example.toml`](config.example.toml). Pour l’utiliser :

```bash
cp config.example.toml server.toml
go run ./cmd/ledmatrix-server -config server.toml -check-config
go run ./cmd/ledmatrix-server -backend memory -config server.toml
```

Sur le Raspberry Pi, remplacer `-backend memory` par `-backend rpi`.

```toml
[HardwareConfig]
Rows                    = 32
Cols                    = 64
ChainLength             = 4
Parallel                = 2
PWMBits                 = 11
PWMLSBNanoseconds       = 130
PWMDitherBits           = 0
Brightness              = 100
ScanMode                = 0
HardwareMapping         = "regular"
ShowRefreshRate         = true
InverseColors           = false
DisableHardwarePulsing  = false
PixelMapperConfig       = "V-mapper"
LimitRefreshRateHz      = 70
Multiplexing            = 0

[RuntimeOptions]
GpioSlowdown            = 5
Daemon                  = 0
DropPrivileges          = -1
DoGpioInit              = true

[HTTPserver]
Addr                    = "detect"
Port                    = 8080
Enabled                 = true
InfoDisplaySeconds      = 5

[Clock]
DefaultMode             = "simple"
```

Les valeurs sont appliquées dans cet ordre :

1. valeurs par défaut internes ;
2. fichier indiqué par `-config` ;
3. options CLI explicitement présentes.

Une clé inconnue ou une valeur hors limites empêche le démarrage. Cela évite
qu’une faute de frappe laisse la dalle fonctionner avec un réglage implicite.
Les clés absentes conservent leur valeur par défaut.

`Clock.DefaultMode` choisit l’horloge affichée au démarrage parmi `simple`,
`fancy` et `round`. L’option serveur `-clock-mode` permet de la remplacer
ponctuellement.

`Addr = "detect"` écoute sur toutes les interfaces (`:Port`). Une adresse IP ou
un nom d’hôte peut être donné pour restreindre l’écoute. `Enabled = false`
initialise le backend puis désactive l’API HTTP ; le processus attend uniquement
un signal d’arrêt.

`InfoDisplaySeconds` définit pendant combien de secondes l’écran technique est
affiché au démarrage et à chaque appel de `POST /v1/display-info`. Une valeur de
`0` désactive cet écran. L’affichage utilise les glyphes bitmap monospace de
`github.com/hajimehoshi/bitmapfont` : ils sont dessinés sur la grille sans
anticrénelage et ne nécessitent aucun fichier de police sur le Raspberry Pi.

`Daemon`, `DropPrivileges` et `DoGpioInit` sont conservés dans le format afin de
rester compatibles avec le fichier initial, mais ne sont pas transmis au
wrapper pour le moment. Seul `GpioSlowdown` est appliqué depuis
`RuntimeOptions`. Le fonctionnement en service sera pris en charge par
`systemd`.

## Développement sur macOS ou Linux

Prérequis : Go 1.22 ou supérieur.

```bash
git clone --recurse-submodules https://github.com/Djoulzy/GoLedMatrix2.git
cd GoLedMatrix2
make test
make build
```

`make build` génère `bin/ledmatrix-server` et `bin/ledmatrix-client`. Il est
également possible de ne compiler qu'un exécutable avec `make build-server` ou
`make build-client`.

Démarrer un serveur sans dalle :

```bash
go run ./cmd/ledmatrix-server -backend memory -rows 32 -cols 64
```

Pour afficher la matrice dans une fenêtre graphique :

```bash
go run ./cmd/ledmatrix-server \
  -backend simulation \
  -config server.toml
```

Le backend `simulation` n'effectue aucune initialisation GPIO ni aucune
communication avec la dalle. L’API HTTP reste identique et `/v1/info` annonce
le backend `simulation`.

La fenêtre graphique est entièrement initialisée avant le démarrage du serveur
HTTP et avant le premier rendu. Cette synchronisation est nécessaire sur macOS
et évite qu’un écran technique envoyé immédiatement soit dessiné sur une
surface OpenGL encore à `0 × 0`.

Au démarrage, le simulateur affiche le même écran technique que la dalle
physique pendant `InfoDisplaySeconds`. La grille de pixels reste visible et
l’écran revient ensuite à la dernière trame reçue, ou à l’horloge si aucun
client n’a encore envoyé d’image. Une trame reçue pendant l’écran technique est
conservée puis affichée automatiquement.

Le simulateur calcule d'abord la géométrie physique avec `Cols × ChainLength` et
`Rows × Parallel`, puis applique dans l'ordre les transformations de
`PixelMapperConfig`. Par exemple, `64 × 32`, `ChainLength = 4`,
`Parallel = 2` et `V-mapper` donnent une surface logique de `128 × 128`.

La taille visuelle des LED peut être ajustée, notamment pour les grandes
matrices :

```bash
go run ./cmd/ledmatrix-server \
  -backend simulation \
  -simulation-pixel-pitch 4 \
  -config server.toml
```

Le mode graphique nécessite une session de bureau active. Sous Linux, installer
les dépendances graphiques avant de compiler le simulateur :

```bash
sudo apt install -y libx11-dev libegl1-mesa-dev libgles2-mesa-dev
```

Les mappers `V-mapper`, `U-mapper`, `StackToRow`, `Rotate` et `Mirror` sont pris
en charge pour le calcul de la géométrie simulée, y compris lorsqu'ils sont
chaînés avec `;`. Un mapper non pris en charge provoque une erreur explicite au
démarrage afin d'éviter d'afficher une géométrie fausse. Fermer la fenêtre ou
appuyer sur `Échap` arrête proprement le serveur.

Envoyer une couleur ou une image aux dimensions exactes :

```bash
go run ./cmd/ledmatrix-client -server http://localhost:8080 -color '#2040ff'
go run ./cmd/ledmatrix-client -server http://localhost:8080 -image frame.png
go run ./cmd/ledmatrix-client -server http://localhost:8080 -clock fancy
```

Les PNG et JPEG sont décodés côté client. Aucun redimensionnement implicite
n’est effectué afin d’éviter les erreurs silencieuses de cadrage.

### Diagnostic du simulateur

Si la fenêtre apparaît mais reste noire :

1. vérifier que le journal contient `backend=simulation` et la géométrie
   attendue ;
2. consulter le contrat détecté avec
   `curl http://localhost:8080/v1/info` ;
3. envoyer une couleur de contrôle avec la commande ci-dessus ;
4. demander à nouveau l’écran technique avec :

```bash
go run ./cmd/ledmatrix-client \
  -server http://localhost:8080 \
  -show-info
```

Une fenêtre noire sans grille dès son ouverture indiquait auparavant que le
premier rendu avait précédé l’initialisation OpenGL. Le serveur attend désormais
le premier événement de dimensionnement de la fenêtre avant de démarrer ; ce
cas est donc corrigé.

## Build et exécution sur Raspberry Pi

La bibliothèque native recommande un système Linux minimal sans interface
graphique. Le câblage, l’alimentation 5 V, le modèle de dalle et les paramètres
de multiplexage doivent être validés avec ses exemples avant ce serveur.

Sur Raspberry Pi OS/Debian :

```bash
sudo apt update
sudo apt install -y build-essential git
git clone --recurse-submodules https://github.com/Djoulzy/GoLedMatrix2.git
cd GoLedMatrix2
cp config.example.toml server.toml
make native
make build-rpi
sudo ./bin/ledmatrix-server \
  -backend rpi \
  -config server.toml
```

Le wrapper cherche normalement ses en-têtes et sa bibliothèque native dans son
propre répertoire `lib/rpi-rgb-led-matrix`, absent des archives du proxy Go.
Le `Makefile` fournit donc `CGO_CFLAGS` et `CGO_LDFLAGS` pour utiliser notre
sous-module épinglé dans `third_party/_rpi-rgb-led-matrix/`. Le préfixe `_`
empêche `go test ./...` d’interpréter par erreur l’arborescence C++ comme des
packages Go.

Options utiles du serveur :

- `-brightness 1..100`
- `-pwm-bits 1..11` : une valeur plus basse augmente le taux de rafraîchissement
  au prix de la profondeur de couleur ;
- `-pwm-lsb-ns 130`
- `-pwm-dither-bits 0..2`
- `-gpio-slowdown 0..60`
- `-pixel-mapper V-mapper`
- `-limit-refresh 70`
- `-multiplexing 0`
- `-show-refresh`
- `-hardware-mapping regular` (ou le mapping correspondant à la carte utilisée).

Le binaire matériel doit être construit sur Linux avec CGO. Le build standard,
sans `-tags rpi`, reste portable et ne lie aucune bibliothèque GPIO.

## Déploiement sur un Raspberry Pi du réseau local

Le déploiement automatisé compile le serveur directement sur le Raspberry Pi,
puis installe le binaire, la configuration et un service `systemd`. Compiler
sur la cible évite les problèmes de cross-compilation CGO et garantit que la
bibliothèque C++ correspond bien à l'architecture du Pi.

### 1. Préparer le Raspberry Pi une seule fois

Activer SSH dans `raspi-config`, puis installer les outils nécessaires :

```bash
sudo apt update
sudo apt install -y build-essential git rsync curl golang-go
go version
```

Go 1.22 ou supérieur est requis. Si la version fournie par Raspberry Pi OS est
plus ancienne, installer une version récente de Go avant de continuer.
Le build matériel exclut le simulateur graphique : aucune bibliothèque
X11/OpenGL n'est nécessaire sur le Raspberry Pi sans écran.

Le script recherche les commandes distantes dans
`/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin`. Cela couvre notamment une
installation officielle de Go dans `/usr/local/go`. Pour une installation dans
un autre répertoire, préciser le chemin :

```bash
make deploy-rpi \
  RPI_HOST=pi@raspberrypi.local \
  RPI_CONFIG=server.toml \
  RPI_REMOTE_PATH=/chemin/vers/go/bin:/usr/local/bin:/usr/bin:/bin
```

En cas de prérequis absent, le script affiche désormais le nom exact de chaque
commande manquante.

Depuis le poste de développement, vérifier la connexion et, de préférence,
installer une clé SSH :

```bash
ssh pi@raspberrypi.local
ssh-copy-id pi@raspberrypi.local
```

Le compte peut être différent de `pi`. Il doit disposer de `sudo`. Une
réservation DHCP pour le Raspberry Pi est recommandée afin que son adresse ne
change pas.

### 2. Préparer et valider la configuration

```bash
cp config.example.toml server.toml
go run ./cmd/ledmatrix-server -config server.toml -check-config
```

Adapter au minimum `Rows`, `Cols`, `ChainLength`, `Parallel`,
`PixelMapperConfig` et les réglages GPIO à la dalle.

### 3. Déployer ou mettre à jour

```bash
make deploy-rpi \
  RPI_HOST=pi@raspberrypi.local \
  RPI_CONFIG=server.toml
```

Pour un port SSH différent :

```bash
make deploy-rpi \
  RPI_HOST=pi@192.168.1.42 \
  RPI_SSH_PORT=2222 \
  RPI_CONFIG=server.toml
```

La commande :

1. copie les sources et le sous-module dans un répertoire temporaire du Pi ;
2. compile `rpi-rgb-led-matrix` et le serveur avec CGO ;
3. valide le fichier TOML ;
4. installe le binaire dans `/opt/goledmatrix2/` ;
5. installe la configuration dans `/etc/goledmatrix2/server.toml` ;
6. active et redémarre `goledmatrix.service` ;
7. vérifie `/healthz` depuis le Pi.

`sudo` peut demander le mot de passe du compte distant pendant l'installation.
Les déploiements suivants utilisent exactement la même commande. Le premier
build télécharge les modules Go et nécessite donc un accès sortant à Internet
depuis le Pi ; aucun accès entrant depuis Internet n'est nécessaire.

La vérification utilise par défaut
`http://127.0.0.1:8080/healthz`. Si le port HTTP configuré est différent,
préciser l'URL correspondante. Si HTTP est désactivé, passer une valeur vide :

```bash
make deploy-rpi RPI_HOST=pi@raspberrypi.local \
  RPI_CONFIG=server.toml \
  RPI_HEALTH_URL=http://127.0.0.1:9090/healthz

make deploy-rpi RPI_HOST=pi@raspberrypi.local \
  RPI_CONFIG=server.toml \
  RPI_HEALTH_URL=
```

Commandes de diagnostic sur le Raspberry Pi :

```bash
sudo systemctl status goledmatrix
sudo journalctl -u goledmatrix -f
curl http://127.0.0.1:8080/v1/info
```

Depuis le poste client, l'API est accessible avec :

```bash
curl http://raspberrypi.local:8080/v1/info
go run ./cmd/ledmatrix-client \
  -server http://raspberrypi.local:8080 \
  -color '#2040ff'
```

Le service écoute selon `HTTPserver.Addr`. Avec `Addr = "detect"`, il écoute sur
toutes les interfaces du Raspberry Pi. Pour conserver l'accès au réseau local
uniquement, ne pas configurer de redirection du port 8080 sur le routeur. Si le
Pi dispose d'un pare-feu, autoriser uniquement le sous-réseau local utilisé.

## Choix de performance

- RGB24 évite la décompression PNG/JPEG sur le Pi.
- La géométrie fixe rend la validation constante et borne strictement la mémoire.
- Un seul goroutine appelle le driver natif.
- La stratégie « dernière trame gagnante » borne la file et la latence.
- La bibliothèque native réalise le double buffering et attend le VSync.
- Une connexion HTTP persistante est réutilisée par le client Go.

La fréquence d’animation réellement soutenable dépend surtout de la taille,
du chaînage, du multiplexage, de `pwm-bits`, du modèle de Pi et de la dalle.
Elle devra être mesurée sur le montage réel avant de fixer une cadence client.

## Commandes de contrôle

```bash
make test
make build
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/...
```

La dernière commande vérifie la séparation portable ; elle ne produit pas le
backend matériel, qui exige CGO et la bibliothèque native.

## Licence

À définir pour le code de ce dépôt. Attention : `rpi-rgb-led-matrix` est sous
GPL-2.0-or-later, tandis que le wrapper Go `zaggash` est sous licence MIT. La
distribution d’un binaire lié à la bibliothèque native doit respecter les
obligations de la GPL.
