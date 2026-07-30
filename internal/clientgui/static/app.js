const $ = (selector) => document.querySelector(selector);

const state = {
  busy: 0,
  geometry: null,
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

async function refreshInfo(silent = false) {
  try {
    const info = await request("/api/info");
    state.geometry = {width: info.width, height: info.height};
    $("#connection").className = "connection online";
    $("#connection-label").textContent = "Serveur connecté";
    $("#backend").textContent = info.backend;
    $("#geometry").textContent = `${info.width} × ${info.height}`;
    $("#format").textContent = info.pixel_format;
    $("#uptime").textContent = humanDuration(info.uptime_seconds);
    const accepted = info.stats?.accepted || 0;
    const rendered = info.stats?.rendered || 0;
    $("#stats").textContent = `${rendered} / ${accepted} trames rendues`;
    if (!silent) toast("État du serveur actualisé");
  } catch (error) {
    $("#connection").className = "connection offline";
    $("#connection-label").textContent = "Serveur hors ligne";
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

bindForm("#color-form", async (form) => {
  setActivity("Envoi de la couleur…");
  await request("/api/color", jsonOptions({color: form.get("color")}));
  setActivity(`Couleur ${form.get("color")} affichée.`);
  toast("Couleur affichée");
});

bindForm("#image-form", async (form) => {
  setActivity("Envoi de l’image…");
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
  event.currentTarget.disabled = true;
  try {
    setActivity("Demande d’affichage des informations…");
    await request("/api/display-info", {method: "POST"});
    setActivity("Informations techniques affichées.");
    toast("Informations techniques affichées");
  } catch (error) {
    toast(error.message, true);
  } finally {
    event.currentTarget.disabled = false;
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

refreshInfo(true);
setInterval(() => refreshInfo(true), 5000);
