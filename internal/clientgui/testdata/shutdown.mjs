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
  for (let i = 0; i < 60; i++) await Promise.resolve();
}

// Only the Headers operations used by app.js are needed by the fetch model.
class TestHeaders {
  constructor(headers = {}) {
    this.values = new Map(Object.entries(headers).map(([key, value]) => [key.toLowerCase(), value]));
  }
  set(key, value) { this.values.set(key.toLowerCase(), value); }
  get(key) { return this.values.get(key.toLowerCase()) ?? null; }
}

const serverA = "http://matrix-a.test:7272";
const serverB = "http://matrix-b.test:7272";

async function harness(info, {confirmed = true, shutdown, infoRequest, brightnessRequest,
  servers = [{url: serverA}], infos = {}} = {}) {
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
    Headers: TestHeaders,
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
      if (path === "/api/servers") return response({servers, initial_server: servers[0]?.url || "", scanning: false});
      const url = options.headers?.get("X-GoLedMatrix-Server");
      if (path === "/api/info") return infoRequest ? infoRequest(url) : response(infos[url] || info);
      if (path === "/api/brightness") return brightnessRequest ? brightnessRequest() : response(JSON.parse(options.body));
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
    async changeServer(url) {
      const target = element("#server-select");
      target.value = url;
      target.listeners.get("change")({target});
      await settle();
    },
    async submitBrightness(value) {
      element("#brightness").value = value;
      await element("#brightness-form").listeners.get("submit")({preventDefault() {}});
      await settle();
    },
    commands: () => requests.filter(request => request.path === "/api/shutdown"),
    buttons: () => buttonSelectors.map(element),
  };
}

const rpi = {backend: "rpi", width: 128, height: 128, brightness: 35, shutdown: {power_off: true}};
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
  assert.equal(h.commands()[0].options.headers.get("Content-Type"), "application/json");
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

// Shutdown follows the selected target and leaves other matrices usable.
{
  const h = await harness(rpi, {servers: [{url: serverA}, {url: serverB}]});
  await h.changeServer(serverB);
  await h.click("#shutdown");
  assert.equal(h.commands()[0].options.headers.get("X-GoLedMatrix-Server"), serverB);
  assert.ok(h.confirmations[0].includes(serverB));
  assert.equal(h.element("#matrix-controls").disabled, true);
  assert.equal(h.element("#server-select").disabled, false);
  const requestCount = h.requests.length;
  await h.submitBrightness(50);
  assert.equal(h.requests.length, requestCount);
  await h.changeServer(serverA);
  assert.equal(h.state.shuttingDown, false);
  assert.equal(h.state.serverReady, true);
  assert.equal(h.element("#matrix-controls").disabled, false);
  assert.equal(h.element("#shutdown").disabled, false);
  count++;
}

// Brightness remains functional and locks the target while being applied.
{
  let resolveBrightness;
  const pending = new Promise(resolve => { resolveBrightness = resolve; });
  const h = await harness(rpi, {brightnessRequest: () => pending});
  const applied = h.submitBrightness(80);
  const command = h.requests.find(request => request.path === "/api/brightness");
  assert.deepEqual(JSON.parse(command.options.body), {brightness: 80});
  assert.equal(command.options.headers.get("X-GoLedMatrix-Server"), serverA);
  assert.equal(h.element("#server-select").disabled, true);
  assert.equal(h.element("#shutdown").disabled, true);
  await h.click("#shutdown");
  assert.equal(h.commands().length, 0);
  resolveBrightness(response({brightness: 80}));
  await applied;
  assert.equal(h.state.brightness, 80);
  assert.equal(h.element("#brightness-value").textContent, "80 %");
  assert.equal(h.element("#server-select").disabled, false);
  assert.equal(h.element("#shutdown").disabled, false);
  count++;
}

// A previous matrix's late info must not replace the new target's capabilities.
{
  let resolveInfo;
  let pollsA = 0;
  const memory = {...rpi, backend: "memory", shutdown: {power_off: false}};
  const h = await harness(rpi, {servers: [{url: serverA}, {url: serverB}], infoRequest: (url) => {
    if (url === serverB) return response(memory);
    if (++pollsA === 1) return response(rpi);
    return new Promise(resolve => { resolveInfo = resolve; });
  }});
  const oldPoll = h.intervals[0]();
  await h.changeServer(serverB);
  resolveInfo(response(rpi));
  await oldPoll;
  assert.equal(h.state.serverInfo.backend, "memory");
  assert.equal(h.element("#shutdown").textContent, "Arrêter le serveur");
  await h.click("#shutdown");
  assert.deepEqual(JSON.parse(h.commands()[0].options.body), {power_off: false});
  assert.equal(h.commands()[0].options.headers.get("X-GoLedMatrix-Server"), serverB);
  count++;
}

console.log(`${count} browser shutdown scenarios passed`);
