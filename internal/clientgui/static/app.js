const $ = (selector) => document.querySelector(selector);

const state = {
  busy: 0,
  geometry: null,
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
  $("#matrix-controls").disabled = !state.serverReady || state.commands > 0;
}

async function selectServer(url) {
  if (url === state.serverURL) return;
  state.serverURL = url;
  state.serverRevision++;
  state.serverReady = false;
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
  updateServerControls();
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

async function refreshInfo(silent = false) {
  if (!state.serverURL) return;
  const serverRevision = state.serverRevision;
  const brightnessRevision = state.brightnessRevision;
  try {
    const info = await request("/api/info");
    if (serverRevision !== state.serverRevision) return;
    state.serverReady = true;
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
    updateServerControls();
    if (!silent) toast("État du serveur actualisé");
  } catch (error) {
    if (serverRevision !== state.serverRevision) return;
    state.serverReady = false;
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
  if (state.brightnessSaving || state.brightness === null) return;
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
    const button = event.currentTarget.querySelector("button[type=submit]");
    button.disabled = true;
    try {
      await action(new FormData(event.currentTarget));
      await refreshInfo(true);
    } catch (error) {
      setActivity("La commande a échoué.");
      toast(error.message, true);
    } finally {
      button.disabled = false;
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
  await request("/api/image", {method: "POST", body: form});
  setActivity("Image affichée.");
  toast("Image affichée");
});

bindForm("#gif-form", async (form) => {
  setActivity("Décodage et redimensionnement du GIF…");
  const metadata = await request("/api/animations", {method: "POST", body: form});
  setActivity(`Animation ${metadata.name} en lecture — ${metadata.frame_count} images.`);
  toast(`Animation « ${metadata.name} » stockée et lancée`);
});

bindForm("#play-form", async (form) => {
  const name = form.get("name");
  setActivity(`Lancement de ${name}…`);
  const metadata = await request("/api/animations/play", jsonOptions({name}));
  setActivity(`Animation ${metadata.name} en lecture.`);
  toast(`Animation « ${metadata.name} » lancée`);
});

$("#display-info").addEventListener("click", async (event) => {
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
    button.disabled = false;
  }
});

$("#refresh").addEventListener("click", () => refreshInfo());
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
