const $ = (selector) => document.querySelector(selector);

const state = {
  busy: 0,
  geometry: null,
  serverInfo: null,
  shuttingDown: false,
  controlsDisabled: false,
  brightness: null,
  brightnessDirty: false,
  brightnessSaving: false,
  brightnessRevision: 0,
  serverURL: "",
  serverRevision: 0,
  serverReady: false,
  serverCount: 0,
  serverInventoryKey: "",
  scanning: true,
  scanRequested: false,
  commands: 0,
};

const catalogues = {
  animations: {
    select: "#animation-select", form: "#play-form", status: "#animation-status",
    label: "animation", plural: "animations", empty: "Aucune animation disponible",
    names: [], revision: 0,
  },
  photos: {
    select: "#photo-select", form: "#photo-display-form", status: "#photo-status",
    label: "photo", plural: "photos", empty: "Aucune photo disponible",
    names: [], revision: 0,
  },
};

function setActivity(message) {
  $("#activity").textContent = message;
}

function toast(message, error = false) {
  const element = $("#toast");
  element.textContent = message;
  element.className = error ? "visible error" : "visible";
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => element.className = "", 3500);
}

async function request(path, options = {}) {
  const matrixRequest = !["/api/servers", "/api/servers/scan", "/api/marquee/fonts"].includes(path);
  const command = matrixRequest && options.method && options.method !== "GET";
  const headers = new Headers(options.headers);
  if (matrixRequest) {
    if (!state.serverURL) throw new Error("Choisissez un serveur LED.");
    headers.set("X-GoLedMatrix-Server", state.serverURL);
  }
  if (command) {
    state.commands++;
    updateServerControls();
  }
  state.busy++;
  document.body.classList.add("busy");
  try {
    const response = await fetch(path, {...options, headers});
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(payload.detail || payload.error || `Erreur HTTP ${response.status}`);
    }
    return payload;
  } finally {
    if (command) {
      state.commands--;
      updateServerControls();
    }
    state.busy--;
    if (!state.busy) document.body.classList.remove("busy");
  }
}

function updateServerControls() {
  $("#server-select").disabled = state.commands > 0 || state.serverCount === 0;
  $("#rescan-servers").disabled = state.scanning || state.scanRequested;
  $("#matrix-controls").disabled = !state.serverReady || state.commands > 0 || state.shuttingDown || state.controlsDisabled;
  for (const catalogue of Object.values(catalogues)) {
    $(catalogue.select).disabled = catalogue.names.length === 0;
    $(catalogue.form + " button").disabled = catalogue.names.length === 0 || !state.serverReady
      || state.commands > 0 || state.shuttingDown || state.controlsDisabled;
  }
  updateShutdownButton();
}

async function selectServer(url) {
  if (url === state.serverURL) return;
  state.serverURL = url;
  state.serverRevision++;
  state.serverReady = false;
  state.serverInfo = null;
  for (const kind of Object.keys(catalogues)) {
    displayCatalogue(kind, [], `Chargement des ${catalogues[kind].plural}…`);
  }
  state.shuttingDown = false;
  $("#refresh").disabled = false;
  state.geometry = null;
  state.brightnessDirty = false;
  state.brightnessRevision++;
  for (const id of ["backend", "geometry", "format", "uptime"]) $("#" + id).textContent = "—";
  $("#stats").textContent = "—";
  $("#connection").className = "connection offline";
  $("#connection-label").textContent = "Connexion…";
  displayTemperatures(null, true);
  displayBrightness(null);
  $("#brightness-value").textContent = "—";
  $("#brightness-status").textContent = "Connexion au serveur…";
  disableServerControls(false);
  setActivity(`Connexion à ${url}…`);
  await refreshInfo(true);
  if (state.serverURL === url && state.serverReady) setActivity(`Serveur sélectionné : ${url}`);
}

function displayServers(inventory) {
  state.scanning = inventory.scanning;
  const servers = [...inventory.servers];
  if (state.serverURL && !servers.some((server) => server.url === state.serverURL)) {
    servers.push({url: state.serverURL, missing: true});
  }
  state.serverCount = servers.length;
  const select = $("#server-select");
  const inventoryKey = JSON.stringify(servers);
  if (inventoryKey !== state.serverInventoryKey) {
    state.serverInventoryKey = inventoryKey;
    select.replaceChildren();
    for (const server of servers) {
      const option = document.createElement("option");
      option.value = server.url;
      option.textContent = [server.url, server.backend, server.width && `${server.width} × ${server.height}`,
        server.missing && "non détecté lors du dernier scan"].filter(Boolean).join(" · ");
      select.append(option);
    }
  }
  if (servers.length === 0) {
    select.replaceChildren();
    const option = document.createElement("option");
    option.value = "";
    option.textContent = state.scanning ? "Recherche de serveurs…" : "Aucun serveur détecté";
    select.append(option);
    $("#connection-label").textContent = option.textContent;
    $("#brightness-status").textContent = "Choisissez un serveur pour régler la luminosité.";
  }
  const selected = state.serverURL || inventory.initial_server || servers[0]?.url || "";
  select.value = selected;
  $("#discovery-status").textContent = state.scanning ? "Recherche de serveurs en cours…"
    : inventory.error ? `Recherche incomplète : ${inventory.error}`
    : inventory.servers.length ? `${inventory.servers.length} adresse(s) disponible(s). Choisissez le serveur à piloter.`
    : "Aucun serveur détecté. Vérifiez le réseau et relancez la recherche.";
  updateServerControls();
  if (selected !== state.serverURL) void selectServer(selected);
}

async function pollServers() {
  try {
    displayServers(await request("/api/servers"));
  } catch (error) {
    $("#discovery-status").textContent = `Chargement des serveurs impossible : ${error.message}`;
    state.scanning = false;
    updateServerControls();
  }
  setTimeout(pollServers, state.scanning ? 1000 : 5000);
}

$("#server-select").addEventListener("change", (event) => {
  void selectServer(event.target.value);
});

$("#rescan-servers").addEventListener("click", async () => {
  state.scanRequested = true;
  updateServerControls();
  try {
    displayServers(await request("/api/servers/scan", {method: "POST"}));
  } catch (error) {
    toast(error.message, true);
  } finally {
    state.scanRequested = false;
    updateServerControls();
  }
});

function humanDuration(seconds) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return [days && `${days}j`, (days || hours) && `${hours}h`, `${minutes}m`].filter(Boolean).join(" ");
}

function displayTemperatures(system, offline = false) {
  const list = $("#temperatures");
  const status = $("#temperature-status");
  list.replaceChildren();
  const temperatures = Array.isArray(system?.temperatures) ? system.temperatures : [];
  for (const temperature of temperatures) {
    if (!Number.isFinite(temperature?.celsius)) continue;
    const reading = document.createElement("div");
    const name = document.createElement("dt");
    name.textContent = temperature.name || temperature.sensor || "Capteur";
    name.title = temperature.sensor || "";
    const value = document.createElement("dd");
    value.textContent = `${temperature.celsius.toLocaleString("fr-FR", {minimumFractionDigits: 1, maximumFractionDigits: 1})} °C`;
    reading.append(name, value);
    list.append(reading);
  }
  status.hidden = list.childElementCount > 0;
  status.textContent = offline ? "Indisponible — serveur hors ligne" : "Indisponible — aucun capteur accessible";
}

function updateShutdownButton() {
  const button = $("#shutdown");
  const hint = $("#shutdown-hint");
  const info = state.serverInfo;
  const powerOff = info?.backend === "rpi";
  button.textContent = powerOff ? "Éteindre le Raspberry Pi" : "Arrêter le serveur";
  button.disabled = !state.serverReady || state.commands > 0 || state.shuttingDown || state.controlsDisabled
    || !info?.shutdown || (powerOff && !info.shutdown.power_off);
  if (!info) {
    hint.textContent = "Connexion au serveur requise pour l’arrêt.";
  } else if (!info.shutdown) {
    hint.textContent = "Mettez à jour le serveur pour activer l’arrêt à distance.";
  } else if (powerOff && !info.shutdown.power_off) {
    hint.textContent = `Extinction indisponible : ${info.shutdown.power_off_unavailable_reason || "permission système manquante"}`;
  } else {
    hint.textContent = powerOff
      ? "Arrête le serveur et la dalle, puis éteint le système. Une confirmation sera demandée."
      : "Arrête uniquement le serveur ; la machine reste allumée. Une confirmation sera demandée.";
  }
}

function disableServerControls(disabled) {
  state.controlsDisabled = disabled;
  for (const button of document.querySelectorAll("main button")) button.disabled = disabled;
  updateServerControls();
}

async function refreshInfo(silent = false) {
  if (!state.serverURL || state.shuttingDown) return;
  const serverRevision = state.serverRevision;
  const brightnessRevision = state.brightnessRevision;
  try {
    const info = await request("/api/info");
    if (serverRevision !== state.serverRevision || state.shuttingDown) return;
    state.serverReady = true;
    state.serverInfo = info;
    state.geometry = {width: info.width, height: info.height};
    $("#connection").className = "connection online";
    $("#connection-label").textContent = "Serveur connecté";
    $("#backend").textContent = info.backend;
    $("#geometry").textContent = `${info.width} × ${info.height}`;
    $("#format").textContent = info.pixel_format;
    $("#uptime").textContent = humanDuration(info.uptime_seconds);
    displayTemperatures(info.system);
    if (brightnessRevision === state.brightnessRevision && !state.brightnessSaving) {
      displayBrightness(info.brightness);
    }
    const accepted = info.stats?.accepted || 0;
    const rendered = info.stats?.rendered || 0;
    $("#stats").textContent = `${rendered} / ${accepted} trames rendues`;
    if (state.controlsDisabled) disableServerControls(false);
    else updateServerControls();
    for (const kind of Object.keys(catalogues)) void refreshCatalogue(kind);
    if (!silent) toast("État du serveur actualisé");
  } catch (error) {
    if (serverRevision !== state.serverRevision || state.shuttingDown) return;
    state.serverInfo = null;
    state.serverReady = false;
    for (const kind of Object.keys(catalogues)) {
      catalogues[kind].revision++;
      displayCatalogue(kind, [], "Liste indisponible — serveur hors ligne.");
    }
    $("#connection").className = "connection offline";
    $("#connection-label").textContent = "Serveur hors ligne";
    displayTemperatures(null, true);
    if (brightnessRevision === state.brightnessRevision && !state.brightnessSaving) {
      displayBrightness(null, true);
    }
    updateServerControls();
    if (!silent) toast(error.message, true);
  }
}

function displayCatalogue(kind, names, message, preferredName) {
  const catalogue = catalogues[kind];
  const select = $(catalogue.select);
  preferredName ??= select.value;
  const changed = names.length !== catalogue.names.length
    || names.some((name, index) => name !== catalogue.names[index]);
  catalogue.names = names;
  if (changed) {
    select.replaceChildren();
    if (names.length === 0) {
      const option = document.createElement("option");
      option.value = "";
      option.textContent = catalogue.empty;
      select.append(option);
    } else {
      for (const name of names) {
        const option = document.createElement("option");
        option.value = name;
        option.textContent = name;
        select.append(option);
      }
    }
  }
  if (names.length > 0) {
    select.value = names.includes(preferredName) ? preferredName : names[0];
  }
  $(catalogue.status).textContent = message;
  updateServerControls();
}

async function refreshCatalogue(kind, preferredName) {
  if (!state.serverReady || state.shuttingDown) return;
  const catalogue = catalogues[kind];
  const serverRevision = state.serverRevision;
  const requestRevision = ++catalogue.revision;
  try {
    const names = await request(`/api/${kind}`);
    if (serverRevision !== state.serverRevision || requestRevision !== catalogue.revision || state.shuttingDown) return;
    if (!Array.isArray(names) || !names.every((name) => typeof name === "string")) {
      throw new Error(`Liste des ${catalogue.plural} invalide.`);
    }
    const message = names.length === 0 ? `Aucune ${catalogue.label} importée sur ce serveur.`
      : `${names.length} ${catalogue.label}(s) disponible(s) sur ce serveur.`;
    displayCatalogue(kind, names, message, preferredName);
  } catch (error) {
    if (serverRevision !== state.serverRevision || requestRevision !== catalogue.revision || state.shuttingDown) return;
    displayCatalogue(kind, [], `Chargement des ${catalogue.plural} impossible : ${error.message}`);
  }
}

function displayBrightness(value, offline = false) {
  const supported = Number.isInteger(value) && value >= 1 && value <= 100;
  state.brightness = supported ? value : null;
  $("#brightness").disabled = !supported || state.brightnessSaving;
  $("#brightness-form button").disabled = !supported || state.brightnessSaving;
  $("#brightness-status").textContent = offline
    ? "Indisponible — serveur hors ligne"
    : supported ? `Luminosité actuelle : ${value} %` : "Réglage indisponible — mettez à jour le serveur";
  if (supported && !state.brightnessDirty && !state.brightnessSaving) {
    $("#brightness").value = value;
    $("#brightness-value").textContent = `${value} %`;
  }
}

$("#brightness").addEventListener("input", (event) => {
  state.brightnessDirty = true;
  $("#brightness-value").textContent = `${event.target.value} %`;
});

$("#brightness-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (state.shuttingDown || state.controlsDisabled || !state.serverReady || state.commands > 0
    || state.brightnessSaving || state.brightness === null) return;
  const value = Number($("#brightness").value);
  state.brightnessSaving = true;
  state.brightnessRevision++;
  displayBrightness(state.brightness);
  try {
    setActivity("Réglage de la luminosité…");
    const result = await request("/api/brightness", jsonOptions({brightness: value}));
    state.brightness = result.brightness;
    state.brightnessDirty = false;
    setActivity(`Luminosité réglée à ${result.brightness} %.`);
    toast("Luminosité appliquée");
  } catch (error) {
    setActivity("Le réglage de la luminosité a échoué.");
    toast(error.message, true);
  } finally {
    state.brightnessSaving = false;
    state.brightnessRevision++;
    displayBrightness(state.brightness);
  }
});

function jsonOptions(body) {
  return {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(body),
  };
}

function bindForm(selector, action) {
  $(selector).addEventListener("submit", async (event) => {
    event.preventDefault();
    if (state.shuttingDown || state.controlsDisabled) return;
    const button = event.currentTarget.querySelector("button[type=submit]");
    button.disabled = true;
    try {
      await action(new FormData(event.currentTarget));
      await refreshInfo(true);
    } catch (error) {
      setActivity("La commande a échoué.");
      toast(error.message, true);
    } finally {
      button.disabled = state.shuttingDown || state.controlsDisabled;
      updateServerControls();
    }
  });
}

bindForm("#clock-form", async (form) => {
  const mode = form.get("mode");
  setActivity(`Activation de l’horloge ${mode}…`);
  await request("/api/clock", jsonOptions({
    mode,
    color1: form.get("color1"),
    color2: form.get("color2"),
  }));
  setActivity(`Horloge ${mode} active.`);
  toast("Horloge activée");
});

bindForm("#marquee-form", async (form) => {
  const cycle = form.get("cycle");
  const verticalBounce = form.get("vertical_bounce") === "true";
  let colors = [];
  if (cycle === "rainbow") {
    colors = ["#ff0000", "#ffff00", "#00ff00", "#00ffff", "#0000ff", "#ff00ff"];
  } else if (cycle === "custom") {
    colors = form.get("palette").split(",").map((color) => color.trim());
    if (colors.length < 2 || colors.length > 16 || colors.some((color) => !/^#[0-9a-f]{6}$/i.test(color))) {
      throw new Error("La palette doit contenir 2 à 16 couleurs au format #RRGGBB.");
    }
  }
  setActivity("Activation du texte défilant…");
  await request("/api/marquee", jsonOptions({
    text: form.get("text"),
    font: form.get("font"),
    size: Number(form.get("size")),
    color: $("#marquee-form input[name=color]").value,
    speed: Number(form.get("speed")),
    bounce: form.get("bounce") === "true",
    vertical_bounce: verticalBounce,
    vertical_bounce_seconds: verticalBounce ? Number(form.get("vertical_bounce_seconds")) : 0,
    color_cycle: colors,
    cycle_seconds: cycle === "none" ? 6 : Number(form.get("cycle_seconds")),
  }));
  setActivity("Texte défilant actif sur le serveur.");
  toast("Texte défilant activé");
});

function updateMarqueeFields() {
  const cycle = $("#marquee-cycle").value;
  $("#marquee-palette-field").hidden = cycle !== "custom";
  $("#marquee-period-field").hidden = cycle === "none";
  $("#marquee-form input[name=palette]").disabled = cycle !== "custom";
  $("#marquee-form input[name=cycle_seconds]").disabled = cycle === "none";
  $("#marquee-form input[name=color]").disabled = cycle !== "none";
}

$("#marquee-cycle").addEventListener("change", updateMarqueeFields);
updateMarqueeFields();

function updateMarqueeVerticalFields() {
  const enabled = $("#marquee-vertical-bounce").value === "true";
  $("#marquee-vertical-period-field").hidden = !enabled;
  $("#marquee-form input[name=vertical_bounce_seconds]").disabled = !enabled;
}

$("#marquee-vertical-bounce").addEventListener("change", updateMarqueeVerticalFields);
updateMarqueeVerticalFields();

async function loadMarqueeFonts() {
  try {
    const fonts = await request("/api/marquee/fonts");
    const select = $("#marquee-form select[name=font]");
    const selected = select.value;
    const groups = new Map();
    for (const font of fonts) {
      if (!groups.has(font.category)) {
        const group = document.createElement("optgroup");
        group.label = font.category;
        groups.set(font.category, group);
      }
      const option = document.createElement("option");
      option.value = font.name;
      option.textContent = font.label;
      groups.get(font.category).append(option);
    }
    select.replaceChildren(...groups.values());
    select.value = selected;
  } catch (error) {
    toast(`Chargement des polices impossible : ${error.message}`, true);
  }
}

loadMarqueeFonts();

bindForm("#color-form", async (form) => {
  setActivity("Envoi de la couleur…");
  await request("/api/color", jsonOptions({color: form.get("color")}));
  setActivity(`Couleur ${form.get("color")} affichée.`);
  toast("Couleur affichée");
});

bindForm("#image-form", async (form) => {
  setActivity("Préparation de l’image : redimensionnement et recadrage centré…");
  const metadata = await request("/api/image", {method: "POST", body: form});
  setActivity(`Photo ${metadata.name} stockée et affichée.`);
  toast(`Photo « ${metadata.name} » stockée et affichée`);
  await refreshCatalogue("photos", metadata.name);
});

bindForm("#photo-display-form", async (form) => {
  const name = form.get("name");
  setActivity(`Affichage de la photo ${name}…`);
  const metadata = await request("/api/photos/display", jsonOptions({name}));
  setActivity(`Photo ${metadata.name} affichée.`);
  toast(`Photo « ${metadata.name} » affichée`);
});

bindForm("#gif-form", async (form) => {
  setActivity("Décodage et redimensionnement du GIF…");
  const metadata = await request("/api/animations", {method: "POST", body: form});
  setActivity(`Animation ${metadata.name} en lecture — ${metadata.frame_count} images.`);
  toast(`Animation « ${metadata.name} » stockée et lancée`);
  await refreshCatalogue("animations", metadata.name);
});

bindForm("#play-form", async (form) => {
  const name = form.get("name");
  setActivity(`Lancement de ${name}…`);
  const metadata = await request("/api/animations/play", jsonOptions({name}));
  setActivity(`Animation ${metadata.name} en lecture.`);
  toast(`Animation « ${metadata.name} » lancée`);
});

$("#display-info").addEventListener("click", async (event) => {
  if (state.shuttingDown || state.controlsDisabled) return;
  const button = event.currentTarget;
  button.disabled = true;
  try {
    setActivity("Demande d’affichage des informations…");
    await request("/api/display-info", {method: "POST"});
    setActivity("Informations techniques affichées.");
    toast("Informations techniques affichées");
  } catch (error) {
    toast(error.message, true);
  } finally {
    button.disabled = state.shuttingDown || state.controlsDisabled;
  }
});

$("#shutdown").addEventListener("click", async (event) => {
  const button = event.currentTarget;
  if (button.disabled || state.shuttingDown) return;
  const serverRevision = state.serverRevision;
  const powerOff = state.serverInfo?.backend === "rpi";
  const confirmation = powerOff
    ? `Éteindre le Raspberry Pi (${state.serverURL}) ? Le serveur et la dalle seront arrêtés proprement, puis le système sera éteint. Attendez la fin de l’arrêt avant de débrancher l’alimentation.`
    : `Arrêter proprement le serveur LED (${state.serverURL}) ? La machine restera allumée. Il faudra relancer le serveur pour utiliser la matrice.`;
  if (!window.confirm(confirmation)) return;
  state.shuttingDown = true;
  disableServerControls(true);
  $("#refresh").disabled = true;
  $("#connection").className = "connection offline";
  $("#connection-label").textContent = "Arrêt en cours…";
  setActivity(powerOff ? "Demande d’extinction du Raspberry Pi…" : "Demande d’arrêt du serveur…");
  try {
    await request("/api/shutdown", jsonOptions({power_off: powerOff}));
    if (serverRevision !== state.serverRevision) return;
    state.serverReady = false;
    updateServerControls();
    const message = powerOff ? "Extinction du Raspberry Pi demandée" : "Arrêt du serveur demandé";
    $("#connection-label").textContent = message;
    displayTemperatures(null, true);
    $("#shutdown-hint").textContent = powerOff
      ? "Attendez la fin de l’arrêt du système avant de débrancher l’alimentation."
      : "Relancez le serveur puis cliquez sur Actualiser pour vous reconnecter.";
    setActivity(`${message}.`);
    toast(message);
    // Keep polling and display commands paused: being offline is expected.
    // The refresh button lets the user reconnect after a subsequent startup.
  } catch (error) {
    if (serverRevision !== state.serverRevision) return;
    state.shuttingDown = false;
    disableServerControls(false);
    setActivity("L’arrêt a échoué.");
    toast(error.message, true);
    await refreshInfo(true);
  } finally {
    if (serverRevision === state.serverRevision) $("#refresh").disabled = false;
  }
});

$("#refresh").addEventListener("click", () => {
  if ($("#refresh").disabled) return;
  state.shuttingDown = false;
  refreshInfo();
});
$("#color-form input[type=color]").addEventListener("input", (event) => {
  $("#color-value").textContent = event.target.value;
});

for (const [inputSelector, labelSelector] of [
  ["#image-form input[type=file]", "#image-file"],
  ["#gif-form input[type=file]", "#gif-file"],
]) {
  $(inputSelector).addEventListener("change", (event) => {
    const name = event.target.files[0]?.name || "Aucun fichier sélectionné";
    $(labelSelector).textContent = name;
  });
}

pollServers();
setInterval(() => refreshInfo(true), 5000);
