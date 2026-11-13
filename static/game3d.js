import * as THREE from 'three';

// World units match the server: x and y span 0..2000 on the ground plane.
// Three.js uses Y as up, so server (x, y) maps to scene (x, height, z = y).
const { id, name } = window.MATCH;
const EYE = 17;
const TICK_MS = 1000 / 30;
const MINIMAP = { x: 10, y: 10, size: 150 };
const lerp = (a, b, t) => a + (b - a) * t;
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));

// ---------- Renderer and scene ----------
const renderer = new THREE.WebGLRenderer({ antialias: true });
renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
renderer.setSize(innerWidth, innerHeight);
document.getElementById('scene-root').appendChild(renderer.domElement);

const scene = new THREE.Scene();
scene.background = new THREE.Color(0x87a7c4);
scene.fog = new THREE.Fog(0x87a7c4, 400, 1600);

const camera = new THREE.PerspectiveCamera(75, innerWidth / innerHeight, 0.5, 5000);
scene.add(new THREE.HemisphereLight(0xffffff, 0x3a4a2a, 0.9));
const sun = new THREE.DirectionalLight(0xffffff, 0.7);
sun.position.set(400, 800, 300);
scene.add(sun);

const ground = new THREE.Mesh(
  new THREE.PlaneGeometry(2000, 2000),
  new THREE.MeshLambertMaterial({ color: 0x3c6b34 })
);
ground.rotation.x = -Math.PI / 2;
ground.position.set(1000, 0, 1000);
scene.add(ground);

// Zone: a translucent red wall. Built once at unit radius, scaled to the live zone.
const zoneMesh = new THREE.Mesh(
  new THREE.CylinderGeometry(1, 1, 220, 96, 1, true),
  new THREE.MeshBasicMaterial({ color: 0xff2020, transparent: true, opacity: 0.22, side: THREE.DoubleSide, depthWrite: false })
);
scene.add(zoneMesh);

// Shared geometries and materials.
const boxGeo = new THREE.BoxGeometry(1, 1, 1);
const playerGeo = new THREE.BoxGeometry(18, 34, 18);
const headGeo = new THREE.SphereGeometry(5, 12, 10);
const lootGeo = new THREE.BoxGeometry(10, 10, 10);
const beamGeo = new THREE.CylinderGeometry(1.5, 1.5, 220, 8, 1, true);
const matObstacle = new THREE.MeshLambertMaterial({ color: 0x6b6b6b });
const matLoot = new THREE.MeshLambertMaterial({ color: 0xf5c542, emissive: 0x402800 });
const matEnemy = new THREE.MeshLambertMaterial({ color: 0xef5350 });
const matMate = new THREE.MeshLambertMaterial({ color: 0x66bb6a });
const matBounty = new THREE.MeshBasicMaterial({ color: 0xff5252, transparent: true, opacity: 0.6, depthWrite: false });
const matPing = new THREE.MeshBasicMaterial({ color: 0xffeb3b, transparent: true, opacity: 0.7, depthWrite: false });
const matTracer = new THREE.LineBasicMaterial({ color: 0xfff3a0 });

// Meshes are keyed by id (players, loot) or by rectangle (obstacles, which the server
// sends only inside the area of interest).
const playerMeshes = new Map();
const lootMeshes = new Map();
const obstacleMeshes = new Map();
const beaconMeshes = new Map();
const tracers = [];

function makePlayerMesh() {
  const g = new THREE.Group();
  const body = new THREE.Mesh(playerGeo, matEnemy);
  body.position.y = 17;
  const head = new THREE.Mesh(headGeo, new THREE.MeshBasicMaterial({ color: 0x000000, transparent: true, opacity: 0 }));
  head.position.y = 30;
  g.add(body, head);
  g.userData.body = body;
  scene.add(g);
  return g;
}

// ---------- Overlay (minimap, kill feed, crosshair, touch controls) ----------
const overlay = document.getElementById('overlay');
const ctx = overlay.getContext('2d');
const hud = document.getElementById('status');

function resize() {
  renderer.setSize(innerWidth, innerHeight);
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
  overlay.width = innerWidth;
  overlay.height = innerHeight;
  STICK.x = 110; STICK.y = innerHeight - 110;
  FIRE.x = innerWidth - 110; FIRE.y = innerHeight - 110;
}
const STICK = { x: 110, y: 0, r: 70, hit: 90 };
const FIRE = { x: 0, y: 0, r: 55, hit: 65 };
addEventListener('resize', resize);
resize();

// ---------- Input ----------
const keys = new Set();
const stick = { dx: 0, dy: 0 }; // stick.dy is screen-down positive
let fireHeld = false;
let shootHeld = false;
let touchActive = false;
let yaw = 0;
let pendingPing = null;
const pointers = new Map(); // pointerId -> { role, x, y }

addEventListener('keydown', e => {
  ensureAudio();
  const k = e.key.toLowerCase();
  keys.add(k);
  if (k === 'm') muted = !muted;
  if (k === 'q' && curr && curr.me.alive) {
    pendingPing = { x: curr.me.x + Math.cos(yaw) * 300, y: curr.me.y + Math.sin(yaw) * 300 };
  }
});
addEventListener('keyup', e => keys.delete(e.key.toLowerCase()));

overlay.style.touchAction = 'none';
const locked = () => document.pointerLockElement === overlay;

function overlayPos(e) {
  const r = overlay.getBoundingClientRect();
  return { x: e.clientX - r.left, y: e.clientY - r.top };
}
const dist = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);
function inMinimap(p) {
  return p.x >= MINIMAP.x && p.x <= MINIMAP.x + MINIMAP.size &&
         p.y >= MINIMAP.y && p.y <= MINIMAP.y + MINIMAP.size;
}

overlay.addEventListener('pointerdown', e => {
  ensureAudio();
  overlay.setPointerCapture(e.pointerId);
  const p = overlayPos(e);
  if (e.pointerType === 'touch') touchActive = true;

  if (e.pointerType === 'touch' && inMinimap(p) && curr) {
    const k = 2000 / MINIMAP.size;
    pendingPing = { x: (p.x - MINIMAP.x) * k, y: (p.y - MINIMAP.y) * k };
    pointers.set(e.pointerId, { role: 'minimap' });
    return;
  }
  if (e.pointerType === 'touch') {
    if (dist(p, STICK) <= STICK.hit) {
      pointers.set(e.pointerId, { role: 'stick', x: p.x, y: p.y });
      updateStick(p);
      return;
    }
    if (dist(p, FIRE) <= FIRE.hit) {
      fireHeld = true;
      pointers.set(e.pointerId, { role: 'fire' });
      return;
    }
    pointers.set(e.pointerId, { role: 'look', x: p.x, y: p.y });
    return;
  }
  // Mouse: first click captures the pointer for mouse-look; while captured, the button fires.
  if (!locked()) {
    overlay.requestPointerLock();
  } else {
    shootHeld = true;
  }
  pointers.set(e.pointerId, { role: 'mouse' });
});

overlay.addEventListener('pointermove', e => {
  if (e.pointerType === 'mouse' && locked()) {
    yaw += e.movementX * 0.0025;
    return;
  }
  const rec = pointers.get(e.pointerId);
  if (!rec) return;
  const p = overlayPos(e);
  if (rec.role === 'look') {
    yaw += (p.x - rec.x) * 0.006;
    rec.x = p.x;
    rec.y = p.y;
  } else if (rec.role === 'stick') {
    updateStick(p);
  }
});

function updateStick(p) {
  const vx = p.x - STICK.x;
  const vy = p.y - STICK.y;
  const len = Math.hypot(vx, vy);
  const k = len > STICK.r ? STICK.r / len : 1;
  stick.dx = (vx * k) / STICK.r;
  stick.dy = (vy * k) / STICK.r;
}

function endPointer(e) {
  const rec = pointers.get(e.pointerId);
  pointers.delete(e.pointerId);
  if (!rec) return;
  if (rec.role === 'stick') { stick.dx = 0; stick.dy = 0; }
  if (rec.role === 'fire') fireHeld = [...pointers.values()].some(r => r.role === 'fire');
  if (rec.role === 'mouse') shootHeld = false;
}
overlay.addEventListener('pointerup', endPointer);
overlay.addEventListener('pointercancel', endPointer);

// ---------- Sound (synthesized, no asset files) ----------
let audioCtx = null;
let muted = false;
let lastShotAt = 0;
let lastEventLast = '';
let prevZoneR = null;
let lastHitSeq = 0;
let hitFlash = { until: 0, kind: '' };

function ensureAudio() {
  if (!audioCtx) audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  if (audioCtx.state === 'suspended') audioCtx.resume();
}
function tone(freq, dur, type = 'square', vol = 0.08, slideTo = null, delay = 0) {
  if (!audioCtx || muted) return;
  const t0 = audioCtx.currentTime + delay;
  const o = audioCtx.createOscillator();
  const g = audioCtx.createGain();
  o.type = type;
  o.frequency.setValueAtTime(freq, t0);
  if (slideTo) o.frequency.exponentialRampToValueAtTime(slideTo, t0 + dur);
  g.gain.setValueAtTime(vol, t0);
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  o.connect(g).connect(audioCtx.destination);
  o.start(t0);
  o.stop(t0 + dur);
}
function noiseBurst(dur, vol = 0.12) {
  if (!audioCtx || muted) return;
  const n = Math.floor(audioCtx.sampleRate * dur);
  const buf = audioCtx.createBuffer(1, n, audioCtx.sampleRate);
  const data = buf.getChannelData(0);
  for (let i = 0; i < n; i++) data[i] = (Math.random() * 2 - 1) * (1 - i / n);
  const src = audioCtx.createBufferSource();
  const g = audioCtx.createGain();
  g.gain.value = vol;
  src.buffer = buf;
  src.connect(g).connect(audioCtx.destination);
  src.start();
}
const SFX = {
  shot: () => { noiseBurst(0.07, 0.12); tone(180, 0.06, 'sawtooth', 0.05, 60); },
  hit: () => tone(520, 0.08, 'triangle', 0.1, 260),
  headshot: () => { tone(880, 0.12, 'sine', 0.12, 440); tone(1320, 0.1, 'sine', 0.08, null, 0.06); },
  kill: () => tone(120, 0.45, 'sawtooth', 0.15, 40),
  pickup: () => { tone(440, 0.08, 'square', 0.06); tone(660, 0.12, 'square', 0.06, null, 0.08); },
  ping: () => tone(990, 0.25, 'sine', 0.08),
  zone: () => tone(300, 0.35, 'triangle', 0.1),
};

// ---------- Networking ----------
const proto = location.protocol === 'https:' ? 'wss' : 'ws';
const ws = new WebSocket(`${proto}://${location.host}/ws/${id}?name=${encodeURIComponent(name)}`);
let prev = null, curr = null, currAt = 0;

ws.onmessage = e => {
  const msg = JSON.parse(e.data);
  if (msg.t !== 'state') return;

  if (curr) {
    if (msg.me.alive && msg.me.hp > curr.me.hp + 1) SFX.pickup();
    if (msg.pings.length > curr.pings.length) SFX.ping();
  }
  const last = msg.events[msg.events.length - 1] ?? '';
  if (last && last !== lastEventLast) {
    lastEventLast = last;
    if (last.includes('eliminated')) SFX.kill();
  }
  if (prevZoneR !== null && msg.zone.r < prevZoneR - 0.5) SFX.zone();
  prevZoneR = msg.zone.r;

  if (msg.hitSeq !== lastHitSeq) {
    lastHitSeq = msg.hitSeq;
    hitFlash = { until: performance.now() + 220, kind: msg.hitKind };
    if (msg.hitKind === 'head') SFX.headshot(); else SFX.hit();
  }

  prev = curr ?? msg;
  curr = msg;
  currAt = performance.now();

  if (!msg.started) hud.textContent = `Waiting for players (${msg.alive}), need 2+ to start`;
  else if (msg.over) hud.textContent = msg.me.place === 1 ? 'Victory Royale' : `Winner: ${msg.winner}`;
  else if (msg.me.alive) hud.textContent = `Alive: ${msg.alive} · HP ${Math.round(msg.me.hp)}`;
  else hud.textContent = `Eliminated · placed #${msg.me.place}`;
};

// Movement is sent in world coordinates, relative to where the player is looking.
setInterval(() => {
  if (ws.readyState !== WebSocket.OPEN) return;
  const fx = Math.cos(yaw), fy = Math.sin(yaw);
  const rx = -Math.sin(yaw), ry = Math.cos(yaw);
  const forward = (keys.has('w') || keys.has('arrowup') ? 1 : 0) - (keys.has('s') || keys.has('arrowdown') ? 1 : 0) - stick.dy;
  const strafe = (keys.has('d') || keys.has('arrowright') ? 1 : 0) - (keys.has('a') || keys.has('arrowleft') ? 1 : 0) + stick.dx;
  let dx = fx * forward + rx * strafe;
  let dy = fy * forward + ry * strafe;
  const len = Math.hypot(dx, dy);
  if (len > 1) { dx /= len; dy /= len; }

  const shoot = shootHeld || fireHeld || keys.has(' ');
  if (shoot && curr && curr.me.alive && performance.now() - lastShotAt > 380) {
    lastShotAt = performance.now();
    SFX.shot();
    spawnTracer();
  }
  let ping = false, px = 0, py = 0;
  if (pendingPing) {
    ping = true;
    px = clamp(pendingPing.x, 0, 2000);
    py = clamp(pendingPing.y, 0, 2000);
    pendingPing = null;
  }
  ws.send(JSON.stringify({ t: 'input', dx, dy, aim: yaw, shoot, ping, px, py }));
}, TICK_MS);

function spawnTracer() {
  if (!curr) return;
  const from = new THREE.Vector3(curr.me.x, EYE - 2, curr.me.y);
  const to = new THREE.Vector3(curr.me.x + Math.cos(yaw) * 500, EYE - 2, curr.me.y + Math.sin(yaw) * 500);
  const line = new THREE.Line(new THREE.BufferGeometry().setFromPoints([from, to]), matTracer);
  scene.add(line);
  tracers.push({ line, until: performance.now() + 80 });
}

// ---------- Scene sync ----------
function syncPlayers(now) {
  const prevById = new Map(prev.players.map(p => [p.id, p]));
  const seen = new Set();
  for (const p of curr.players) {
    seen.add(p.id);
    let mesh = playerMeshes.get(p.id);
    if (!mesh) { mesh = makePlayerMesh(); playerMeshes.set(p.id, mesh); }
    const old = prevById.get(p.id) ?? p;
    mesh.position.set(lerp(old.x, p.x, 0.5), 0, lerp(old.y, p.y, 0.5));
    const team = curr.me.team !== 0 && p.team === curr.me.team;
    mesh.userData.body.material = team ? matMate : matEnemy;
    mesh.visible = true;
  }
  for (const [pid, mesh] of playerMeshes) {
    if (!seen.has(pid)) { scene.remove(mesh); playerMeshes.delete(pid); }
  }
}

function syncObstacles() {
  const seen = new Set();
  for (const r of curr.obstacles) {
    const key = `${r.x},${r.y},${r.w},${r.h}`;
    seen.add(key);
    if (!obstacleMeshes.has(key)) {
      const m = new THREE.Mesh(boxGeo, matObstacle);
      m.scale.set(r.w, 60, r.h);
      m.position.set(r.x + r.w / 2, 30, r.y + r.h / 2);
      scene.add(m);
      obstacleMeshes.set(key, m);
    }
  }
  for (const [key, m] of obstacleMeshes) {
    if (!seen.has(key)) { scene.remove(m); obstacleMeshes.delete(key); }
  }
}

function syncLoot(now) {
  const seen = new Set();
  for (const l of curr.loot) {
    seen.add(l.id);
    let m = lootMeshes.get(l.id);
    if (!m) {
      m = new THREE.Mesh(lootGeo, matLoot);
      scene.add(m);
      lootMeshes.set(l.id, m);
    }
    m.position.set(l.x, 6 + Math.sin(now / 300 + l.id) * 2, l.y);
    m.rotation.y = now / 800;
  }
  for (const [lid, m] of lootMeshes) {
    if (!seen.has(lid)) { scene.remove(m); lootMeshes.delete(lid); }
  }
}

// Bounty beacons: a red beam over every revealed player, shown to everyone.
function syncBeacons() {
  const seen = new Set();
  for (const b of curr.revealed) {
    seen.add(b.id);
    let m = beaconMeshes.get(b.id);
    if (!m) {
      m = new THREE.Mesh(beamGeo, matBounty);
      m.position.y = 110;
      scene.add(m);
      beaconMeshes.set(b.id, m);
    }
    m.position.x = b.x;
    m.position.z = b.y;
  }
  for (const [bid, m] of beaconMeshes) {
    if (!seen.has(bid)) { scene.remove(m); beaconMeshes.delete(bid); }
  }
}

const pingMeshes = [];
function syncPings() {
  while (pingMeshes.length) scene.remove(pingMeshes.pop());
  for (const pg of curr.pings) {
    const m = new THREE.Mesh(beamGeo, matPing);
    m.scale.set(0.6, 1, 0.6);
    m.position.set(pg.x, 110, pg.y);
    scene.add(m);
    pingMeshes.push(m);
  }
}

function updateCamera(meX, meY) {
  camera.position.set(meX, EYE, meY);
  camera.lookAt(meX + Math.cos(yaw) * 100, EYE, meY + Math.sin(yaw) * 100);
  const r = curr.zone.r;
  zoneMesh.position.set(curr.zone.x, 110, curr.zone.y);
  zoneMesh.scale.set(r, 1, r);
}

// ---------- Overlay drawing ----------
function drawCrosshair(bloom) {
  const gap = 6 + bloom * 200;
  const cx = overlay.width / 2, cy = overlay.height / 2;
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.9)';
  ctx.lineWidth = 2;
  ctx.beginPath();
  ctx.moveTo(cx - gap - 8, cy); ctx.lineTo(cx - gap, cy);
  ctx.moveTo(cx + gap, cy);     ctx.lineTo(cx + gap + 8, cy);
  ctx.moveTo(cx, cy - gap - 8); ctx.lineTo(cx, cy - gap);
  ctx.moveTo(cx, cy + gap);     ctx.lineTo(cx, cy + gap + 8);
  ctx.stroke();
}

function drawHitMarker(now) {
  if (now > hitFlash.until) return;
  const cx = overlay.width / 2, cy = overlay.height / 2;
  const head = hitFlash.kind === 'head';
  ctx.strokeStyle = head ? '#ff5252' : '#fff';
  ctx.lineWidth = 3;
  ctx.beginPath();
  for (const [dx, dy] of [[-1, -1], [1, -1], [-1, 1], [1, 1]]) {
    ctx.moveTo(cx + dx * 8, cy + dy * 8);
    ctx.lineTo(cx + dx * 16, cy + dy * 16);
  }
  ctx.stroke();
  if (head) {
    ctx.fillStyle = '#ff5252';
    ctx.font = 'bold 16px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText('HEADSHOT', cx, cy - 26);
  }
}

function drawMinimap() {
  const { x, y, size } = MINIMAP;
  const k = size / 2000;
  ctx.save();
  ctx.fillStyle = 'rgba(0, 0, 0, 0.5)';
  ctx.fillRect(x, y, size, size);
  ctx.beginPath();
  ctx.rect(x, y, size, size);
  ctx.clip();

  ctx.strokeStyle = '#fff';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.arc(x + curr.zone.x * k, y + curr.zone.y * k, curr.zone.r * k, 0, Math.PI * 2);
  ctx.stroke();

  ctx.fillStyle = '#666';
  for (const r of curr.obstacles) ctx.fillRect(x + r.x * k, y + r.y * k, Math.max(2, r.w * k), Math.max(2, r.h * k));
  for (const pg of curr.pings) {
    ctx.fillStyle = '#ffeb3b';
    ctx.beginPath(); ctx.arc(x + pg.x * k, y + pg.y * k, 3, 0, Math.PI * 2); ctx.fill();
  }
  for (const mate of curr.players) {
    if (mate.team === 0 || mate.team !== curr.me.team) continue;
    ctx.fillStyle = '#66bb6a';
    ctx.beginPath(); ctx.arc(x + mate.x * k, y + mate.y * k, 3.5, 0, Math.PI * 2); ctx.fill();
  }
  for (const b of curr.revealed) {
    ctx.fillStyle = '#ff5252';
    ctx.beginPath(); ctx.arc(x + b.x * k, y + b.y * k, 4, 0, Math.PI * 2); ctx.fill();
  }
  if (curr.me.alive) {
    ctx.fillStyle = '#4fc3f7';
    ctx.beginPath(); ctx.arc(x + curr.me.x * k, y + curr.me.y * k, 4, 0, Math.PI * 2); ctx.fill();
    // Facing indicator.
    ctx.strokeStyle = '#4fc3f7';
    ctx.beginPath();
    ctx.moveTo(x + curr.me.x * k, y + curr.me.y * k);
    ctx.lineTo(x + (curr.me.x + Math.cos(yaw) * 60) * k, y + (curr.me.y + Math.sin(yaw) * 60) * k);
    ctx.stroke();
  }
  ctx.restore();
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.4)';
  ctx.strokeRect(x, y, size, size);
}

function drawTouchControls() {
  ctx.save();
  ctx.globalAlpha = 0.35;
  ctx.fillStyle = '#000';
  ctx.beginPath(); ctx.arc(STICK.x, STICK.y, STICK.r, 0, Math.PI * 2); ctx.fill();
  ctx.fillStyle = '#fff';
  ctx.beginPath(); ctx.arc(STICK.x + stick.dx * STICK.r, STICK.y + stick.dy * STICK.r, 26, 0, Math.PI * 2); ctx.fill();
  ctx.globalAlpha = fireHeld ? 0.7 : 0.35;
  ctx.fillStyle = '#ef5350';
  ctx.beginPath(); ctx.arc(FIRE.x, FIRE.y, FIRE.r, 0, Math.PI * 2); ctx.fill();
  ctx.globalAlpha = 1;
  ctx.fillStyle = '#fff';
  ctx.font = 'bold 16px sans-serif';
  ctx.textAlign = 'center';
  ctx.fillText('FIRE', FIRE.x, FIRE.y + 6);
  ctx.restore();
}

function drawOverlay(now) {
  ctx.clearRect(0, 0, overlay.width, overlay.height);
  if (!curr) return;

  ctx.font = '14px sans-serif';
  ctx.textAlign = 'right';
  ctx.fillStyle = '#fff';
  [`Alive: ${curr.alive}`, ...curr.events].forEach((line, i) => ctx.fillText(line, overlay.width - 10, 20 + i * 18));

  if (curr.me.alive) {
    drawCrosshair(curr.me.bloom);
    drawHitMarker(now);
  }
  drawMinimap();
  if (touchActive) drawTouchControls();

  ctx.textAlign = 'center';
  if (curr.started && !curr.me.alive && !curr.over) {
    ctx.fillStyle = '#fff';
    ctx.font = '32px sans-serif';
    ctx.fillText(`Eliminated — #${curr.me.place}`, overlay.width / 2, overlay.height / 2);
  }
  if (curr.over) {
    ctx.fillStyle = 'rgba(0, 0, 0, 0.55)';
    ctx.fillRect(0, overlay.height / 2 - 60, overlay.width, 120);
    ctx.fillStyle = '#ffd54f';
    ctx.font = 'bold 36px sans-serif';
    ctx.fillText(curr.me.place === 1 ? 'Victory Royale' : `${curr.winner} wins`, overlay.width / 2, overlay.height / 2);
    ctx.fillStyle = '#fff';
    ctx.font = '18px sans-serif';
    ctx.fillText(`You placed #${curr.me.place}`, overlay.width / 2, overlay.height / 2 + 30);
  }
}

// ---------- Main loop ----------
function frame(now) {
  requestAnimationFrame(frame);

  // Expire tracers.
  for (let i = tracers.length - 1; i >= 0; i--) {
    if (now > tracers[i].until) {
      scene.remove(tracers[i].line);
      tracers.splice(i, 1);
    }
  }

  if (curr) {
    const t = Math.min(1, (now - currAt) / TICK_MS);
    const meX = lerp(prev.me.x, curr.me.x, t);
    const meY = lerp(prev.me.y, curr.me.y, t);
    syncPlayers(now);
    syncObstacles();
    syncLoot(now);
    syncBeacons();
    syncPings();
    // The local player is never drawn: the camera is their eyes.
    updateCamera(meX, meY);
  }

  renderer.render(scene, camera);
  drawOverlay(now);
}
requestAnimationFrame(frame);
