// Run with: node internal/clientgui/testdata/shutdown.mjs
// A small DOM model exercises the actual embedded browser script, without
// contacting a Raspberry Pi or requiring a browser/test-framework dependency.
import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import vm from "node:vm";

const source = await readFile(new URL("../static/app.js", import.meta.url), "utf8");

function response(payload, status = 200) {
  return {ok: status < 400, status, json: async () => payload};
}

async function settle() {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

async function harness(info, {confirmed = true, shutdown, infoRequest} = {}) {
  const elements = new Map();
  function element(selector) {
    if (elements.has(selector)) return elements.get(selector);
    const current = {
      textContent: "", value: "", disabled: false, hidden: false,
      children: [], listeners: new Map(),
      addEventListener(type, listener) { this.listeners.set(type, listener); },
      querySelector(child) { return element(`${selector} ${child}`); },
      append(...children) { this.children.push(...children); },
      replaceChildren(...children) { this.children = children; },
      get childElementCount() { return this.children.length; },
    };
    elements.set(selector, current);
    return current;
  }
  const buttonSelectors = ["#shutdown", "#display-info", ...["clock", "marquee", "color", "image", "gif", "play"].map(name => `#${name}-form button[type=submit]`)];
  element("#shutdown").disabled = true;
  element("#marquee-cycle").value = "none";
  element("#marquee-vertical-bounce").value = "false";
  const requests = [], confirmations = [], intervals = [];
  const context = vm.createContext({
    document: {
      querySelector: element,
      querySelectorAll: () => buttonSelectors.map(element),
      createElement: name => element(`created-${name}-${elements.size}`),
      body: {classList: {add() {}, remove() {}}},
    },
    window: {confirm(message) { confirmations.push(message); return confirmed; }},
    setTimeout: () => 1,
    clearTimeout: () => {},
    setInterval: callback => { intervals.push(callback); return 1; },
    async fetch(path, options = {}) {
      requests.push({path, options});
      if (path === "/api/marquee/fonts") return response([]);
      if (path === "/api/info") return infoRequest ? infoRequest() : response(info);
      if (path === "/api/shutdown") return shutdown ? shutdown() : response({status: "shutting_down"}, 202);
      throw new Error(`Unexpected request: ${path}`);
    },
  });
  vm.runInContext(source, context);
  await settle();
  return {
    element, requests, confirmations, intervals,
    state: vm.runInContext("state", context),
    async click(selector) {
      const target = element(selector);
      const result = target.listeners.get("click")({currentTarget: target, target});
      await result;
      await settle();
    },
    commands: () => requests.filter(request => request.path === "/api/shutdown"),
    buttons: () => buttonSelectors.map(element),
  };
}

const rpi = {backend: "rpi", width: 128, height: 128, shutdown: {power_off: true}};
let count = 0;

// Cancellation leaves both the server and the UI unchanged.
{
  const h = await harness(rpi, {confirmed: false});
  assert.equal(h.element("#shutdown").textContent, "Éteindre le Raspberry Pi");
  assert.equal(h.element("#shutdown").disabled, false);
  await h.click("#shutdown");
  assert.equal(h.commands().length, 0);
  assert.equal(h.state.shuttingDown, false);
  assert.match(h.confirmations[0], /débrancher l’alimentation/);
  count++;
}

// Acknowledgement pauses commands and polling, without claiming the Pi is
// already powered off. Double clicks do not submit duplicate commands.
{
  let resolveShutdown;
  const pending = new Promise(resolve => { resolveShutdown = resolve; });
  const h = await harness(rpi, {shutdown: () => pending});
  const clicked = h.click("#shutdown");
  assert.equal(h.state.shuttingDown, true);
  assert.equal(h.element("#refresh").disabled, true);
  assert.ok(h.buttons().every(button => button.disabled));
  assert.deepEqual(JSON.parse(h.commands()[0].options.body), {power_off: true});
  assert.equal(h.commands()[0].options.headers["Content-Type"], "application/json");
  const requestCount = h.requests.length;
  await h.intervals[0]();
  assert.equal(h.requests.length, requestCount);
  resolveShutdown(response({status: "shutting_down"}, 202));
  await clicked;
  assert.equal(h.element("#connection-label").textContent, "Extinction du Raspberry Pi demandée");
  assert.equal(h.element("#refresh").disabled, false);
  assert.match(h.element("#shutdown-hint").textContent, /fin de l’arrêt/);
  await h.click("#shutdown");
  assert.equal(h.commands().length, 1);
  await h.intervals[0]();
  assert.equal(h.requests.length, requestCount);
  await h.click("#refresh");
  assert.equal(h.state.shuttingDown, false);
  assert.equal(h.element("#connection-label").textContent, "Serveur connecté");
  assert.ok(h.buttons().every(button => !button.disabled));
  count++;
}

// Non-RPi backends only stop the server, never the client workstation.
for (const backend of ["memory", "simulation"]) {
  const h = await harness({...rpi, backend, shutdown: {power_off: false}});
  assert.equal(h.element("#shutdown").textContent, "Arrêter le serveur");
  await h.click("#shutdown");
  assert.deepEqual(JSON.parse(h.commands()[0].options.body), {power_off: false});
  assert.match(h.confirmations[0], /machine restera allumée/);
  assert.equal(h.element("#connection-label").textContent, "Arrêt du serveur demandé");
  count++;
}

// Older servers and missing power-off permission are explained, not silently
// downgraded to a server-only shutdown on the Raspberry Pi.
for (const info of [
  {...rpi, shutdown: undefined},
  {...rpi, shutdown: {power_off: false, power_off_unavailable_reason: "permission missing"}},
]) {
  const h = await harness(info);
  assert.equal(h.element("#shutdown").disabled, true);
  assert.match(h.element("#shutdown-hint").textContent, /Mettez à jour|permission missing/);
  await h.click("#shutdown");
  assert.equal(h.commands().length, 0);
  assert.equal(h.confirmations.length, 0);
  count++;
}

// A rejected command displays the reason and leaves polling/commands usable.
{
  const h = await harness(rpi, {shutdown: () => response({detail: "permission denied"}, 503)});
  await h.click("#shutdown");
  assert.equal(h.state.shuttingDown, false);
  assert.equal(h.commands().length, 1);
  assert.match(h.element("#toast").textContent, /permission denied/);
  assert.equal(h.element("#connection-label").textContent, "Serveur connecté");
  assert.equal(h.element("#shutdown").disabled, false);
  count++;
}

// An info request already in flight cannot overwrite the shutdown message.
{
  let resolveInfo;
  let polls = 0;
  const h = await harness(rpi, {infoRequest: () => {
    if (++polls === 1) return response(rpi);
    return new Promise(resolve => { resolveInfo = resolve; });
  }});
  const oldPoll = h.intervals[0]();
  await h.click("#shutdown");
  resolveInfo(response(rpi));
  await oldPoll;
  assert.equal(h.element("#connection-label").textContent, "Extinction du Raspberry Pi demandée");
  assert.equal(h.element("#shutdown").disabled, true);
  count++;
}

// Failed reconnection after shutdown keeps display controls disabled.
{
  let polls = 0;
  const h = await harness(rpi, {infoRequest: () => ++polls === 1 ? response(rpi) : response({error: "offline"}, 502)});
  await h.click("#shutdown");
  await h.click("#refresh");
  assert.equal(h.element("#connection-label").textContent, "Serveur hors ligne");
  assert.ok(h.buttons().every(button => button.disabled));
  count++;
}

console.log(`${count} browser shutdown scenarios passed`);
