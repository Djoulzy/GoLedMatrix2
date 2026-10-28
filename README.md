# GoLedMatrix2

Serveur d’affichage léger pour matrices LED RGB HUB75 pilotées par Raspberry Pi,
et client Go chargé de préparer puis d’envoyer les images.

Le projet s’appuie sur
[`rpi-rgb-led-matrix`](https://github.com/hzeller/rpi-rgb-led-matrix), dont la
version est épinglée dans le sous-module `third_party/_rpi-rgb-led-matrix`.
Un adaptateur CGO local utilise directement son API C et conserve le buffer
hors écran renvoyé par chaque échange au VSync.

## État du projet

Fondations disponibles :

- serveur HTTP versionné, client en ligne de commande et panneau de contrôle
  web local ;
- découverte des serveurs du réseau local avec l'option client `-scan` ;
- réglage de luminosité à chaud depuis l'API, la CLI et l'interface web ;
- protocole RGB24 brut, sans décodage ni composition sur le Raspberry Pi ;
- file à une seule trame logique : si plusieurs images arrivent pendant un
  rendu, seule la plus récente est conservée ;
- backend matériel Linux/Raspberry Pi isolé derrière les build tags
  `linux,cgo,rpi` ;
- backend mémoire portable pour macOS, Linux, le développement et les tests ;
- simulateur graphique représentant chaque pixel de la matrice logique ;
- écran technique temporaire au démarrage, rendu avec une police bitmap ;
- trois horloges (`simple`, `fancy`, `round`) sélectionnables côté
  serveur ou client et affichées par défaut lorsqu’aucune trame client n’est
  active ;
- prétraitement des GIF côté client, stockage persistant des trames RGB24 sur
  le Pi et lecture autonome temporisée ;
- configuration TOML stricte du matériel, du runtime GPIO et du serveur HTTP ;
- arrêt propre, limites de taille, timeouts HTTP et tests avec race detector ;
- dépendances Go et bibliothèque C++ épinglées.

À venir :

- authentification et TLS pour une exposition hors réseau local ;
- bibliothèque de composition avancée côté client au-delà du support GIF ;
- métriques et mesures de débit/latence sur le matériel cible ;
- paquet Debian et procédure de désinstallation.

## Architecture

Le client compose une image à la géométrie exacte de la dalle, la convertit en
RGB24 puis l’envoie. Le serveur valide la taille, remplace la trame en attente et
copie la trame RGB24 dans le buffer hors écran puis l'échange au prochain VSync.

`rpi-rgb-led-matrix` conserve ensuite cette image dans son buffer natif et
rafraîchit continuellement les GPIO. Il n’existe volontairement **aucune boucle
Go qui redessine une image inchangée** : elle consommerait du CPU et introduirait
de la gigue sans améliorer la stabilité.

```text
composition/PNG (client) -> RGB24 -> HTTP -> dernière trame -> copie/VSync
GIF -> composition/resize client -> paquet RGB24 temporisé -> stockage/lecture
                                                              -> scan GPIO natif
GUI web -> client Go local -> mêmes commandes HTTP et prétraitements
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
  "brightness": 100,
  "base_urls": ["http://192.168.0.18:7272"],
  "started_at": "2026-07-28T10:00:00Z",
  "uptime_seconds": 42,
  "stats": {"accepted": 0, "rendered": 0, "failed": 0},
  "shutdown": {"power_off": false},
  "system": {
    "temperatures": [{"sensor": "thermal_zone0", "name": "cpu-thermal", "celsius": 48.5}],
    "sampled_at": "2026-07-28T10:00:40Z"
  }
}
```

`base_urls` fournit au client les adresses actuellement utilisables pour joindre
le serveur. Avec une écoute sur toutes les interfaces, la liste est recalculée
à chaque demande : les adresses source choisies par le routage du système sont
prioritaires, puis viennent les autres adresses des interfaces actives.
La détection interroge la pile réseau sans envoyer de paquet ni dépendre d'un
service Internet. L'écran technique affiche la première adresse de cette liste,
au démarrage et lors de `POST /v1/display-info`. Une écoute limitée à une adresse
précise n'annonce que cette adresse. L'adresse SSH utilisée pour le déploiement
n'intervient pas dans cette détection.

`system.temperatures` contient les capteurs thermiques internes accessibles sur
le serveur Linux, indépendamment du backend d'affichage. Le serveur détecte les
zones `/sys/class/thermal/thermal_zone*`, lit leur `type` et convertit leur `temp`
de millièmes de degré en °C. Le numéro de zone ne détermine pas le type de capteur.
Il ne s'agit pas de la température ambiante.

Un relevé est effectué au démarrage puis toutes les cinq secondes en tâche de
fond, sans lecture de fichiers pendant les requêtes HTTP ni dans le rendu LED.
`sampled_at` indique l'heure du dernier relevé, y compris lorsqu'aucun capteur
n'a pu être lu. Les capteurs absents, illisibles ou invalides sont ignorés : la
liste est vide si aucun n'est accessible, sans bloquer le serveur ni conserver
d'anciennes températures. Hors Linux, la liste est vide et `sampled_at` absent.
Aucune bibliothèque supplémentaire ni commande externe n'est nécessaire.

`shutdown` annonce le support de l'arrêt à distance. `power_off` indique si le
serveur peut aussi éteindre son système Linux/RPi sans demande de mot de passe.
Si cette opération n'est pas disponible, `power_off_unavailable_reason` en donne
la raison. Les anciens serveurs n'annoncent pas ce champ.

### `POST /v1/shutdown`

- `Content-Type: application/json` ;
- corps `{"power_off": true}` pour arrêter proprement le serveur et éteindre
  le Raspberry Pi ; `{"power_off": false}` pour arrêter seulement le serveur ;
- réponse `202 Accepted` avec `{"status":"shutting_down","power_off":true}`.

La réponse confirme la prise en compte de la demande, pas la fin de l'extinction.
Les requêtes déjà en cours disposent de cinq secondes pour se terminer avant
fermeture de leurs connexions. Le serveur arrête ses producteurs et attend la
fin du rendu, puis ferme le pilote natif, qui éteint les LED et libère les GPIO.
Seulement ensuite, `systemctl --no-ask-password poweroff` demande l'arrêt normal
du système : aucun arrêt forcé ni coupure brutale de l'alimentation.
Voir aussi la [documentation de l'arrêt système de systemd](https://www.freedesktop.org/software/systemd/man/254/systemd-halt.service.html).

L'extinction système est réservée au backend `rpi` sous Linux avec `systemd`.
Elle est refusée si les permissions nécessaires manquent, sans arrêter le
serveur. Les demandes identiques sont idempotentes ; changer le mode d'arrêt
après acceptation renvoie `409 Conflict`. Si le nettoyage ou la commande finale
échoue après acceptation, le processus signale l'erreur dans ses journaux et
termine en échec ; une erreur de nettoyage empêche l'extinction système.

Le déploiement installe une règle `sudoers`, validée par `visudo`, qui autorise
uniquement le compte `daemon` (utilisé par défaut par le pilote natif après
abandon des privilèges root) à exécuter cette commande exacte sans mot de passe.
Le serveur ne conserve pas des droits root pour cette fonction. Un serveur déjà
installé doit être **redéployé** pour recevoir la nouvelle règle.
Lors d'une installation manuelle, utiliser `deploy/install-rpi.sh` après le
build matériel, ou faire configurer cette permission limitée par
l'administrateur système.

L'API n'a pas encore d'authentification : toute personne pouvant la joindre peut
demander un arrêt. La réserver à un réseau local de confiance et ne pas l'exposer
sur Internet. Le JSON est obligatoire et aucun accès CORS n'est accordé, afin
d'empêcher un formulaire provenant d'un autre site de déclencher un arrêt.

### `PUT /v1/brightness`

Règle immédiatement la luminosité globale de l'affichage, de **1 à 100 %**, sans
redémarrer le serveur. Le corps utilise `Content-Type: application/json` :

```json
{"brightness": 35}
```

La réponse est `200 OK` avec `{"brightness":35}` lorsque le pilote a appliqué
le réglage. La valeur courante est aussi exposée par `GET /v1/info`.
Une valeur absente, non entière ou hors limites est rejetée sans modifier la
luminosité. Le réglage conserve le mode actif : image fixe, horloge, texte
défilant, animation ou écran technique temporaire.

Sur Raspberry Pi, le réglage utilise la fonction native de
`rpi-rgb-led-matrix`. L'image visible est recopiée une seule fois dans le buffer
hors écran puis échangée au VSync pour appliquer aussi la nouvelle luminosité
aux images fixes. L'accès au pilote reste sérialisé avec les autres rendus,
sans boucle de rafraîchissement Go supplémentaire. Le simulateur et le backend
mémoire modulent les composantes RGB en conservant la trame d'origine.

```bash
./bin/ledmatrix-client -no-gui -server http://192.168.0.18:7272 -brightness 35
```

Le réglage est conservé jusqu'à la prochaine modification ou à l'arrêt du
serveur. Au redémarrage, `HardwareConfig.Brightness` (ou l'option serveur
`-brightness`) fournit la valeur initiale ; le fichier TOML n'est pas modifié
par l'API.

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
go run ./cmd/ledmatrix-client -no-gui \
  -server http://192.168.0.18:7272 \
  -show-info
```

### `POST /v1/clock?mode={simple|fancy|round}&color1={hex}&color2={hex}`

Sélectionne la variante, réactive l’horloge du serveur et abandonne la dernière
trame client. `color1` et `color2`, au format `#RRGGBB`, sont facultatives et
remplacent les couleurs configurées. La réponse est `202 Accepted` avec le mode
et les couleurs réellement appliqués. Les trois variantes sont :

- `simple` : heure `HH:MM:SS` mobile avec la police `Perform` ;
- `fancy` : heures et minutes superposées avec la police `HappyBomb` et les
  couleurs orange et cyan d’origine ;
- `round` : reproduction d’`OfficeRound` avec la police `TickingTimebomb`,
  cadran blanc, progression circulaire rouge des secondes et heure centrée.

Les trois modes utilisent le rendu TTF anticrénelé de `gg`, conformément aux
visuels d’origine. Le rendu pixel-perfect reste réservé à l’écran technique.

Commande équivalente avec le client :

```bash
go run ./cmd/ledmatrix-client -no-gui \
  -server http://192.168.0.18:7272 \
  -clock round \
  -clock-color1 '#ff0000' \
  -clock-color2 '#ffffff'
```

### `POST /v1/marquee`

Lance un texte défilant sur fond noir. Par défaut, il est centré verticalement
et défile de droite à gauche.
Le serveur vise 60 images/seconde. En mode boucle, le texte est répété après sa
sortie complète à gauche, même lorsque le client est fermé. Les positions entre
pixels sont interpolées pour lisser le mouvement, y compris à des vitesses non
entières.
La position dépend du temps réellement écoulé : une trame retardée ne provoque
pas le rejeu d'anciennes positions. La cadence effective dépend du matériel ;
les échanges de buffers sur le Pi sont toujours effectués au VSync.
Toutes les polices `.ttf` et `.otf`
présentes dans `assets/ttf` sont intégrées aux exécutables, en plus de Go Regular,
Go Bold et Go Mono ; aucun fichier de police n'est requis sur le Raspberry Pi.

Le corps utilise `Content-Type: application/json` :

```json
{
  "text": "Bonjour à tous !",
  "font": "regular",
  "size": 16,
  "color": "#FFFFFF",
  "speed": 30,
  "bounce": false,
  "vertical_bounce": false,
  "color_cycle": ["#FF0000", "#00FF00", "#0000FF"],
  "cycle_seconds": 6
}
```

Seul `text` est obligatoire : une ligne UTF-8 de 4096 octets maximum. Les
autres paramètres prennent les valeurs par défaut ci-dessus, sauf
`color_cycle`, vide par défaut (couleur fixe). `font` accepte un nom de police
(par exemple `Flashback`, `Perform` ou `Pixel_NES`), son chemin relatif à
`assets/ttf` (par exemple `marquee/Flashback.ttf`), ainsi que `regular`, `bold`
ou `mono`. Les noms sont insensibles à la casse ; en cas de doublon, utiliser
le chemin relatif. `size` est une taille en pixels de 2 à 256 ; un texte plus haut que
la matrice est rogné. `speed` va de 1 à 500 pixels/seconde. Le cycle contient
2 à 16 couleurs `#RRGGBB` et effectue des transitions progressives sur une
période complète de `cycle_seconds` secondes (0,1 à 3600). La réponse est
`202 Accepted` avec les réglages effectivement appliqués.

```bash
# Catalogue local complet, sans connexion au serveur
./bin/ledmatrix-client -no-gui -list-marquee-fonts

./bin/ledmatrix-client -no-gui -server http://192.168.0.18:7272 \
  -marquee 'Bonjour à tous !' \
  -marquee-font Flashback -marquee-size 20 \
  -marquee-color '#ff8337' -marquee-speed 40

./bin/ledmatrix-client -no-gui -server http://192.168.0.18:7272 \
  -marquee 'Un texte arc-en-ciel' \
  -marquee-cycle rainbow -marquee-cycle-seconds 6

./bin/ledmatrix-client -no-gui -server http://192.168.0.18:7272 \
  -marquee 'Une palette personnalisée' \
  -marquee-cycle '#ff8337,#7be0de,#ffffff' -marquee-cycle-seconds 4
```

Avec `bounce: true`, le texte fait des allers-retours horizontaux : s'il tient
dans la dalle, il reste entièrement visible et rebondit aux bords ; s'il est
plus large, le début puis la fin du texte sont affichés avant chaque inversion.
Un texte exactement aussi large que la dalle reste fixe. Les couleurs continuent
leur cycle pendant les rebonds. La valeur par défaut `false` conserve le
défilement en boucle de droite à gauche.

```bash
./bin/ledmatrix-client -no-gui -server http://192.168.0.107:7272 \
  -marquee 'Bonjour !' -marquee-bounce -marquee-speed 30
```

Avec `vertical_bounce: true`, le texte continue son défilement horizontal tout
en montant et descendant suivant une sinusoïde. Il commence au centre et utilise
toute la hauteur disponible autour du texte, sans ajouter de rognage vertical.
Si le texte est aussi haut que la dalle ou plus haut, il reste centré verticalement
(avec le rognage habituel si nécessaire). `vertical_bounce_seconds` règle la durée
d'un aller-retour complet, de 0,1 à 3600 secondes, avec 4 secondes par défaut lorsque
l'effet est activé. Le mouvement vertical est interpolé entre les pixels et sa
phase reste continue, même lorsque le texte quitte la dalle ou reboucle.

L'oscillation verticale est indépendante de `bounce` : elle peut accompagner le
défilement classique de droite à gauche ou le rebond horizontal existant, avec
une couleur fixe ou un cycle de couleurs. Dans la GUI, choisir « Trajectoire
verticale → Sinusoïdale (haut / bas) » et régler la durée de l'aller-retour.

```bash
./bin/ledmatrix-client -no-gui -server http://192.168.0.107:7272 \
  -marquee 'Bonjour !' -marquee-vertical-bounce \
  -marquee-vertical-bounce-seconds 4 -marquee-speed 30
```

Un nouveau marquee, une image/couleur, une animation ou l'horloge remplace le
défilement actif. Une requête invalide conserve l'affichage en cours. L'écran
technique est temporaire : le défilement continue et réapparaît à son expiration.

### `GET /v1/animations`

Retourne les noms des animations stockées sur le serveur, triés par ordre
alphabétique, par exemple `["demo", "feu"]`. Un stockage vide retourne `[]`.
La GUI utilise cette liste pour choisir une animation à relancer.

### `PUT /v1/animations/{name}`

Stocke une animation prétraitée au format
`application/vnd.goledmatrix.animation+gzip`. Le paquet contient uniquement la
géométrie, les trames RGB24, leurs durées et le nombre de boucles. Par défaut,
l’animation démarre après son installation ; `?play=false` la stocke sans la
lancer.

Le client décode le GIF, applique transparence et modes de disposal, adapte
l’image à la matrice en conservant son ratio, puis téléverse le résultat :

```bash
go run ./cmd/ledmatrix-client -no-gui \
  -server http://192.168.0.18:7272 \
  -gif animation.gif \
  -animation-name demo
```

`-animation-loops 0` force une boucle infinie. `-1`, la valeur par défaut,
conserve le comportement indiqué dans le GIF.

### `POST /v1/animations/{name}/play`

Relance une animation déjà stockée, sans renvoyer le GIF ni les trames :

```bash
go run ./cmd/ledmatrix-client -no-gui \
  -server http://192.168.0.18:7272 \
  -play-animation demo
```

Une trame client, un marquee, une autre animation ou le retour à l’horloge interrompt la
lecture active. L’écran technique reste temporaire et restaure ensuite le mode
actif.

Les erreurs utilisent `application/problem+json`. Le serveur ne décode ni GIF,
PNG, JPEG ou HEIC et ne redimensionne aucune image : ces opérations coûteuses restent
du côté client. Seul le paquet de trames préparées est compressé pour le
transfert et le stockage.

## Découverte des serveurs sur le réseau local

L'option `-scan` du client liste les serveurs GoLedMatrix2 accessibles sur les
sous-réseaux IPv4 des interfaces actives (Ethernet, Wi-Fi, etc.), sans avoir à
connaître leur adresse :

```bash
go run ./cmd/ledmatrix-client -no-gui -scan
# Ou après make build-client :
./bin/ledmatrix-client -no-gui -scan
```

Le résultat indique l'URL à utiliser avec `-server`, la géométrie logique de la
matrice et le backend de chaque adresse détectée, dans l'ordre des adresses IP.
Le client interroge uniquement `GET /v1/info` et vérifie le contrat RGB24 v1 :
un autre service HTTP n'est pas affiché et le contenu des matrices n'est pas
modifié. Les serveurs existants sont compatibles sans mise à jour.

Le port sondé est **7272** par défaut. Pour un autre port ou pour limiter la
recherche à un sous-réseau précis :

```bash
./bin/ledmatrix-client -no-gui -scan -scan-port 9090
./bin/ledmatrix-client -no-gui -scan -scan-network 192.168.0.0/24
./bin/ledmatrix-client -no-gui -scan -scan-timeout 1s -timeout 3m
```

Le masque réel de chaque interface détermine le sous-réseau, sans supposer un
`/24`. Les sous-réseaux qui se chevauchent sont dédupliqués ; les adresses réseau
et broadcast sont exclues, sauf pour les préfixes `/31` et `/32`. L'adresse du
client est incluse. La boucle locale et les interfaces point à point (notamment
les tunnels VPN) ne sont pas parcourues automatiquement ; `-scan-network`
permet de les sélectionner explicitement.

La recherche lance au maximum 64 requêtes simultanées, avec un délai de 500 ms
par adresse (`-scan-timeout`) et une limite totale de deux minutes (`-timeout`).
Un dépassement de la limite totale ou `Ctrl+C` affiche les résultats déjà
obtenus et signale que le scan est incomplet. Un scan terminé sans résultat
affiche un message explicite.

Le scan est limité à 65 536 adresses IPv4 au total ; au-delà, le client demande
de sélectionner un sous-réseau plus petit avec `-scan-network`. Seuls les
serveurs HTTP joignables sur le port choisi sont détectés : IPv6, HTTPS, les
autres ports et les machines bloquées par un pare-feu ne sont pas couverts.
Les proxys HTTP configurés dans l'environnement ne sont pas utilisés.
`-scan` est une action autonome du mode `-no-gui`, à utiliser sans `-image`,
`-clock` ou autre commande d'affichage ; `-server` n'intervient pas dans cette
commande de découverte.

## Interface web du client

Le client lance **par défaut** le panneau de contrôle web sur le port **8080**
de l'IP de son interface réseau par défaut (`-gui-listen detect:8080`). L'adresse
complète à ouvrir est affichée au démarrage, par exemple
`http://192.168.0.42:8080`. L'option `-gui` est remplacée par `-no-gui`, qui active
les commandes sans interface.

La détection utilise l'adresse source choisie par le routage du système, en
privilégiant IPv4 puis IPv6. Elle ne dépend ni de l'ordre des interfaces, ni de
l'adresse du Raspberry Pi, et ne transmet aucun paquet ni requête DNS. En
l'absence de route utilisable, le panneau reste accessible localement sur
`http://127.0.0.1:8080`. L'adresse est choisie au démarrage : relancer le client
si l'IP de l'interface change.

```bash
# Interface web et découverte automatique des serveurs
go run ./cmd/ledmatrix-client
# Ou après make build-client :
./bin/ledmatrix-client

# Utiliser directement un serveur connu, sans scan au démarrage
./bin/ledmatrix-client -server http://192.168.0.18:7272

# Adapter la découverte au réseau ou à un port serveur différent
./bin/ledmatrix-client -scan-network 192.168.0.0/24 -scan-port 7272

# Restreindre explicitement l'interface web à la machine locale
./bin/ledmatrix-client -gui-listen 127.0.0.1:8080
```

Sans `-server`, le client recherche les serveurs en arrière-plan dès son
lancement. L'interface reste accessible pendant la recherche et affiche sa
progression, les résultats ou une erreur explicite. Le premier résultat est
sélectionné automatiquement. Le sélecteur « Serveur sélectionné » permet de
passer d'une matrice à l'autre sans relancer le client. Chaque entrée indique
l'adresse, le backend et la géométrie détectée ; un serveur accessible par
plusieurs interfaces réseau peut donc avoir plusieurs entrées.

Le bouton « Rechercher les serveurs » relance la découverte, y compris lorsqu'un
serveur a été fourni avec `-server`. L'adresse fournie explicitement reste
proposée. Une adresse déjà sélectionnée reste utilisable si un scan ultérieur
ne la retrouve plus ; elle est alors indiquée comme non détectée. Le changement
de serveur actualise les informations, les températures et la luminosité ; une
modification de luminosité non appliquée est remise à la valeur du nouveau
serveur. Les commandes sont désactivées tant qu'aucun serveur n'est joignable.

Le choix est propre à chaque onglet du navigateur. Une commande en cours garde
sa cible pendant toute son exécution ; le sélecteur est temporairement désactivé
pendant les envois. Le navigateur contacte uniquement la passerelle Go locale,
qui utilise les mêmes commandes HTTP et prétraitements que le client headless.
Le serveur LED conserve son port par défaut **7272**, distinct du port de
l'interface web locale **8080**.

L'interface permet de :

- consulter la connexion, la géométrie, le backend et les statistiques ;
- consulter les températures internes du serveur, actualisées toutes les cinq
  secondes, ou l'état « Indisponible » si aucun capteur n'est accessible ;
- régler la luminosité globale de 1 à 100 % avec le curseur « Luminosité » et
  le bouton « Appliquer » ; la valeur courante est actualisée toutes les cinq
  secondes sans écraser une modification du curseur en cours ;
- choisir les trois horloges et leurs couleurs ;
- faire défiler un texte en boucle ou avec rebond horizontal, avec oscillation
  verticale sinusoïdale optionnelle, police, taille, vitesse, couleur fixe,
  arc-en-ciel ou palette personnalisée et durée du cycle ;
- envoyer une couleur, une image PNG/JPEG/HEIC ou un GIF ;
- prétraiter, nommer, stocker et lancer un GIF sur le Pi ;
- relancer une animation persistante depuis la liste déroulante des animations
  du serveur sélectionné, actualisée automatiquement après un import ;
- demander l’affichage temporaire des informations techniques ;
- éteindre proprement le Raspberry Pi, avec confirmation, depuis le panneau
  « Outils » (en mémoire/simulation, seul le serveur est arrêté).

Après une demande d'arrêt, les commandes et l'actualisation automatique sont
suspendues pour le serveur sélectionné : le serveur hors ligne est attendu.
Le client local reste ouvert et le sélecteur permet de piloter une autre matrice.
Après redémarrage du Pi ou du serveur, cliquer sur « Actualiser » pour se
reconnecter. Attendre la fin de l'arrêt du système avant de débrancher
l'alimentation. Si les permissions d'extinction manquent ou si le serveur est
trop ancien, le bouton est désactivé et indique la raison ; le client ne se
rabat pas silencieusement sur un simple arrêt du serveur RPi.

Le choix de police du texte défilant est alimenté par le catalogue embarqué et
regroupé par famille (`digital`, `fixed`, `modern`, `marquee` et `Go`). Les
polices ajoutées dans `assets/ttf` sont proposées après recompilation.

Le réglage de luminosité nécessite un serveur intégrant `PUT /v1/brightness`.
Face à un serveur plus ancien, le curseur est désactivé et un message invite
à mettre à jour le serveur.

Les images fixes envoyées par la GUI sont automatiquement redimensionnées pour
remplir la matrice en conservant leur aspect ratio, puis recadrées au centre.
L'excédent est coupé symétriquement sur les côtés ou en haut et en bas : aucune
déformation ni bande ajoutée. Les PNG transparents sont composés sur fond noir.
Le client accepte PNG, JPEG et HEIC/HEIF et limite les images décodées à
64 mégapixels. L'option CLI `-image` conserve son exigence de dimensions exactes.

Le HEIC est converti localement en PNG avant ce traitement : sur macOS, le
client utilise `sips`, fourni avec le système. Sur les autres plateformes,
installer les outils de [libheif](https://github.com/strukturag/libheif) sur la
machine qui lance la GUI, avec `heif-dec` ou `heif-convert` accessible dans le
`PATH`. Aucune dépendance HEIC n'est nécessaire sur le serveur LED. Une erreur
explicite est affichée si le convertisseur n'est pas disponible ; les fichiers
temporaires sont supprimés après la conversion.

Les GIF conservent leur ratio et sont automatiquement adaptés à la géométrie
avec des bandes si nécessaire, comme avec l’option CLI `-gif`.

La GUI est donc accessible depuis le réseau de l'interface choisie par défaut.
Le projet n'ayant pas encore d'authentification, réserver cet accès à un réseau
de confiance. Pour garder un accès strictement local, utiliser
`-gui-listen 127.0.0.1:8080`. Une adresse explicite reste prioritaire : par
exemple `-gui-listen 192.168.0.42:9090` pour une interface précise, ou
`-gui-listen 0.0.0.0:8080` pour toutes les interfaces IPv4.

## Client sans interface

Ajouter `-no-gui` pour exécuter une commande puis quitter :

```bash
./bin/ledmatrix-client -no-gui -server http://192.168.0.18:7272 -brightness 35
./bin/ledmatrix-client -no-gui -server http://192.168.0.18:7272 -color '#2040ff'
./bin/ledmatrix-client -no-gui -scan
```

Sans `-server`, les commandes d'affichage lancent aussi une découverte. Si une
seule adresse est trouvée, elle est utilisée ; si plusieurs adresses sont
trouvées, le client les liste et demande de choisir avec `-server`, sans envoyer
de commande d'affichage. Un scan incomplet ou sans résultat ne sélectionne pas
de cible. Les commandes locales `-scan` et `-list-marquee-fonts` restent autonomes.
Les options `-scan-port`, `-scan-network`, `-scan-timeout` et `-timeout` règlent
également cette découverte automatique.

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
RowAddressType          = 0
RGBSequence             = "RGB"
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
Port                    = 7272
Enabled                 = true
InfoDisplaySeconds      = 5

[Clock]
DefaultMode             = "simple"
Color1                  = ""
Color2                  = ""

[Animations]
Directory               = "animations"
MaxUploadMB             = 128
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
ponctuellement. `Clock.Color1` et `Clock.Color2` acceptent des couleurs
`#RRGGBB`. Une valeur vide conserve les couleurs d’origine du mode :

- `simple` : blanc et blanc (`HH:MM`, puis secondes) ;
- `fancy` : orange et cyan (heures, puis minutes) ;
- `round` : rouge et blanc (heure/progression, puis cadran).

`Animations.Directory` définit le répertoire des fichiers `.glma` persistants.
`Animations.MaxUploadMB` borne la taille compressée d’un téléversement. Avec le
service `systemd` fourni, le répertoire relatif `animations` se trouve dans
`/var/lib/goledmatrix2/animations`.

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

`RowAddressType` correspond directement à l’option native
`--led-row-addr-type` de `rpi-rgb-led-matrix`. Les valeurs acceptées sont :

- `0` : adressage direct standard (valeur par défaut) ;
- `1` : panneaux adressés par lignes A/B avec registre à décalage ;
- `2` : sélection directe d’une ligne ABCD ;
- `3` : adressage ABC avec registre à décalage ;
- `4` : adressage ABC décalé avec D/E directs (SM5266) ;
- `5` : adressage par registre B707, variante souvent plus rapide que `3`.

Ce réglage est transmis uniquement au backend matériel. Il ne modifie ni la
géométrie ni le rendu du simulateur.

`RGBSequence` correspond à `--led-rgb-sequence` et corrige l’ordre physique des
canaux lorsqu’une dalle mélange les couleurs. Sa valeur doit être une
permutation de `RGB`, par exemple `RGB` (défaut), `BGR` ou `GRB`. Ce paramètre
est lui aussi propre au backend matériel ; les trames HTTP restent toujours au
format RGB24 standard.

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
go run ./cmd/ledmatrix-client -no-gui -server http://localhost:7272 -color '#2040ff'
go run ./cmd/ledmatrix-client -no-gui -server http://localhost:7272 -image frame.png
go run ./cmd/ledmatrix-client -no-gui -server http://localhost:7272 \
  -clock fancy -clock-color1 '#ff8337' -clock-color2 '#7be0de'
go run ./cmd/ledmatrix-client -no-gui -server http://localhost:7272 \
  -gif animation.gif -animation-name demo
```

Les PNG et JPEG sont décodés côté client sans redimensionnement implicite. Les
GIF utilisés avec `-gif` suivent explicitement une politique « contain » :
ratio conservé, centrage et bandes noires si nécessaire.

### Diagnostic du simulateur

Si la fenêtre apparaît mais reste noire :

1. vérifier que le journal contient `backend=simulation` et la géométrie
   attendue ;
2. consulter le contrat détecté avec
   `curl http://localhost:7272/v1/info` ;
3. envoyer une couleur de contrôle avec la commande ci-dessus ;
4. demander à nouveau l’écran technique avec :

```bash
go run ./cmd/ledmatrix-client -no-gui \
  -server http://localhost:7272 \
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

L'adaptateur CGO et le `Makefile` utilisent les en-têtes et la bibliothèque
du sous-module épinglé dans `third_party/_rpi-rgb-led-matrix/`. Le préfixe `_`
empêche `go test ./...` d’interpréter par erreur l’arborescence C++ comme des
packages Go.

Options utiles du serveur :

- `-brightness 1..100`
- `-pwm-bits 1..11` : une valeur plus basse augmente le taux de rafraîchissement
  au prix de la profondeur de couleur ;
- `-pwm-lsb-ns 130`
- `-pwm-dither-bits 0..2`
- `-row-addr-type 0..5` : méthode native d’adressage des lignes
  (`--led-row-addr-type`) ; `0` convient aux panneaux à adressage direct ;
- `-rgb-sequence RGB` : ordre physique des canaux LED
  (`--led-rgb-sequence`) ; accepte toute permutation de `RGB` ;
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
6. valide et installe la permission limitée d'extinction système ;
7. active et redémarre `goledmatrix.service` ;
8. vérifie `/healthz` depuis le Pi.

`sudo` peut demander le mot de passe du compte distant pendant l'installation.
Les déploiements suivants utilisent exactement la même commande. Le premier
build télécharge les modules Go et nécessite donc un accès sortant à Internet
depuis le Pi ; aucun accès entrant depuis Internet n'est nécessaire.

La vérification utilise par défaut
`http://127.0.0.1:7272/healthz`. Si le port HTTP configuré est différent,
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
curl http://127.0.0.1:7272/v1/info
```

Depuis le poste client, l'API est accessible avec :

```bash
curl http://raspberrypi.local:7272/v1/info
go run ./cmd/ledmatrix-client -no-gui \
  -server http://raspberrypi.local:7272 \
  -color '#2040ff'
```

Le service écoute selon `HTTPserver.Addr`. Avec `Addr = "detect"`, il écoute sur
toutes les interfaces du Raspberry Pi. Pour conserver l'accès au réseau local
uniquement, ne pas configurer de redirection du port 7272 sur le routeur. Si le
Pi dispose d'un pare-feu, autoriser uniquement le sous-réseau local utilisé.

## Choix de performance

- RGB24 évite la décompression PNG/JPEG sur le Pi.
- La géométrie fixe rend la validation constante et borne strictement la mémoire.
- Un seul goroutine appelle le driver natif.
- La stratégie « dernière trame gagnante » borne la file et la latence.
- Les trames RGB24 sont copiées en bloc dans le buffer hors écran ; le buffer
  renvoyé par l'échange au VSync devient le buffer d'écriture suivant. Aucune
  trame n'est composée dans le buffer actif pendant le balayage de la dalle.
- Une connexion HTTP persistante est réutilisée par le client Go.
- Les animations autonomes sont lues depuis des trames RGB24 déjà préparées ;
  le Pi ne décode jamais le GIF et utilise des échéances monotones pour limiter
  la dérive temporelle.

La fréquence d’animation réellement soutenable dépend surtout de la taille,
du chaînage, du multiplexage, de `pwm-bits`, du modèle de Pi et de la dalle.
Elle devra être mesurée sur le montage réel avant de fixer une cadence client.

## Commandes de contrôle

```bash
make test
make test-rpi-binding
make build
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/...
```

La dernière commande vérifie la séparation portable ; elle ne produit pas le
backend matériel, qui exige CGO et la bibliothèque native.

`make test` inclut les tests du binding CGO, aussi disponibles séparément avec
`make test-rpi-binding`. Ils compilent l'adaptateur de production contre un faux
pilote C et les vrais en-têtes, sans accéder aux GPIO : alternance des buffers,
ordre RGB24, géométrie après mapping, réglages matériels et arrêt sont vérifiés.
Un compilateur C et les en-têtes du sous-module initialisé sont nécessaires.
Le tag `rpistub` est réservé à ces tests et ne doit pas être utilisé pour un
serveur réel.

## Licence

À définir pour le code de ce dépôt. Attention : `rpi-rgb-led-matrix` est sous
GPL-2.0-or-later. La distribution d’un binaire lié à la bibliothèque native doit
respecter les obligations de la GPL.
