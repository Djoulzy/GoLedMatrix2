const $ = (selector) => document.querySelector(selector);

const state = {
  busy: 0,
  geometry: null,
  serverInfo: null,
  shuttingDown: false,
  controlsDisabled: false,
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
  state.busy++;
  document.body.classList.add("busy");
  try {
    const response = await fetch(path, options);
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(payload.detail || payload.error || `Erreur HTTP ${response.status}`);
    }
    return payload;
  } finally {
    state.busy--;
    if (!state.busy) document.body.classList.remove("busy");
  }
}

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
  button.disabled = state.shuttingDown || !info?.shutdown || (powerOff && !info.shutdown.power_off);
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
  updateShutdownButton();
}

async function refreshInfo(silent = false) {
  if (state.shuttingDown) return;
  try {
    const info = await request("/api/info");
    // An earlier poll must not overwrite a shutdown accepted in the meantime.
    if (state.shuttingDown) return;
    state.serverInfo = info;
    state.geometry = {width: info.width, height: info.height};
    $("#connection").className = "connection online";
    $("#connection-label").textContent = "Serveur connecté";
    $("#backend").textContent = info.backend;
    $("#geometry").textContent = `${info.width} × ${info.height}`;
    $("#format").textContent = info.pixel_format;
    $("#uptime").textContent = humanDuration(info.uptime_seconds);
    displayTemperatures(info.system);
    const accepted = info.stats?.accepted || 0;
    const rendered = info.stats?.rendered || 0;
    $("#stats").textContent = `${rendered} / ${accepted} trames rendues`;
    if (state.controlsDisabled) disableServerControls(false);
    else updateShutdownButton();
    if (!silent) toast("État du serveur actualisé");
  } catch (error) {
    if (state.shuttingDown) return;
    state.serverInfo = null;
    $("#connection").className = "connection offline";
    $("#connection-label").textContent = "Serveur hors ligne";
    displayTemperatures(null, true);
    updateShutdownButton();
    if (!silent) toast(error.message, true);
  }
}

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
  const powerOff = state.serverInfo?.backend === "rpi";
  const confirmation = powerOff
    ? "Éteindre le Raspberry Pi ? Le serveur et la dalle seront arrêtés proprement, puis le système sera éteint. Attendez la fin de l’arrêt avant de débrancher l’alimentation."
    : "Arrêter proprement le serveur LED ? La machine restera allumée. Il faudra relancer le serveur pour utiliser la matrice.";
  if (!window.confirm(confirmation)) return;
  state.shuttingDown = true;
  disableServerControls(true);
  $("#refresh").disabled = true;
  $("#connection").className = "connection offline";
  $("#connection-label").textContent = "Arrêt en cours…";
  setActivity(powerOff ? "Demande d’extinction du Raspberry Pi…" : "Demande d’arrêt du serveur…");
  try {
    await request("/api/shutdown", jsonOptions({power_off: powerOff}));
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
    state.shuttingDown = false;
    disableServerControls(false);
    setActivity("L’arrêt a échoué.");
    toast(error.message, true);
    await refreshInfo(true);
  } finally {
    $("#refresh").disabled = false;
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

refreshInfo(true);
setInterval(() => refreshInfo(true), 5000);
