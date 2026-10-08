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

bindForm("#marquee-form", async (form) => {
  const cycle = form.get("cycle");
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
