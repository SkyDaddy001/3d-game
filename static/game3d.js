import * as THREE from 'three';
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js';

// World units match the server: x and y span 0..2000 on the ground plane.
// Three.js uses Y as up, so server (x, y) maps to scene (x, height, z = y).
const { id, name } = window.MATCH;
const EYE = 17;
const TICK_MS = 1000 / 30;
const MINIMAP = { x: 10, y: 10, size: 150 };
const lerp = (a, b, t) => a + (b - a) * t;
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));

// ---------- Renderer, lighting, and sky ----------
const renderer = new THREE.WebGLRenderer({ antialias: true });
renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
renderer.setSize(innerWidth, innerHeight);
renderer.outputColorSpace = THREE.SRGBColorSpace;
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.05;
renderer.shadowMap.enabled = true;
renderer.shadowMap.type = THREE.PCFSoftShadowMap;
document.getElementById('scene-root').appendChild(renderer.domElement);

const scene = new THREE.Scene();
const pmrem = new THREE.PMREMGenerator(renderer);
scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
scene.fog = new THREE.Fog(0xb9cfe0, 350, 1500);

// Sky: a vertical gradient on the background.
const skyTex = makeCanvasTexture(4, 256, (g, w, h) => {
  const grad = g.createLinearGradient(0, 0, 0, h);
  grad.addColorStop(0, '#2f5d9e');
  grad.addColorStop(0.55, '#8fb8d8');
  grad.addColorStop(1, '#d9e6ee');
  g.fillStyle = grad;
  g.fillRect(0, 0, w, h);
}, false);
scene.background = skyTex;

const camera = new THREE.PerspectiveCamera(72, innerWidth / innerHeight, 0.5, 5000);
scene.add(camera);

const hemi = new THREE.HemisphereLight(0xdbeaff, 0x3b4a2c, 0.6);
scene.add(hemi);
const sun = new THREE.DirectionalLight(0xfff1d6, 2.4);
sun.castShadow = true;
sun.shadow.mapSize.set(2048, 2048);
Object.assign(sun.shadow.camera, { left: -420, right: 420, top: 420, bottom: -420, near: 10, far: 2000 });
sun.shadow.bias = -0.0008;
sun.shadow.normalBias = 0.6;
scene.add(sun, sun.target);

// ---------- Procedural textures (no image files needed) ----------
function makeCanvasTexture(repeat, draw, tile = true) {
  const size = 256;
  const c = document.createElement('canvas');
  c.width = size;
  c.height = size;
  draw(c.getContext('2d'), size, size);
  const t = new THREE.CanvasTexture(c);
  if (tile) t.wrapS = t.wrapT = THREE.RepeatWrapping;
  t.repeat.set(repeat, repeat);
  t.colorSpace = THREE.SRGBColorSpace;
  t.anisotropy = renderer.capabilities.getMaxAnisotropy();
  return t;
}

function speckle(g, w, h, count, colors, minSize, maxSize) {
  for (let i = 0; i < count; i++) {
    g.fillStyle = colors[(Math.random() * colors.length) | 0];
    const s = minSize + Math.random() * (maxSize - minSize);
    g.fillRect(Math.random() * w, Math.random() * h, s, s);
  }
}

const grassTex = makeCanvasTexture(60, (g, w, h) => {
  g.fillStyle = '#3f6b2e';
  g.fillRect(0, 0, w, h);
  speckle(g, w, h, 3000, ['rgba(120,160,70,0.5)', 'rgba(30,60,20,0.5)', 'rgba(170,190,100,0.3)'], 1, 3);
});

const concreteTex = makeCanvasTexture(2, (g, w, h) => {
  g.fillStyle = '#8f8d87';
  g.fillRect(0, 0, w, h);
  speckle(g, w, h, 2600, ['rgba(60,60,58,0.35)', 'rgba(200,198,190,0.3)'], 1, 3);
  g.strokeStyle = 'rgba(50,50,48,0.5)';
  g.lineWidth = 2;
  for (let i = 0; i < 4; i++) {
    g.beginPath();
    g.moveTo(Math.random() * w, Math.random() * h);
    g.lineTo(Math.random() * w, Math.random() * h);
    g.stroke();
  }
});

// Building facade: concrete with a grid of dark windows that catch light.
const facadeTex = makeCanvasTexture(1, (g, w, h) => {
  g.fillStyle = '#b7b1a5';
  g.fillRect(0, 0, w, h);
  speckle(g, w, h, 1200, ['rgba(80,75,70,0.25)', 'rgba(230,225,215,0.25)'], 1, 2);
  const cols = 4, rows = 6;
  for (let r = 0; r < rows; r++) {
    for (let c = 0; c < cols; c++) {
      const x = 14 + c * ((w - 28) / cols), y = 14 + r * ((h - 28) / rows);
      const grad = g.createLinearGradient(x, y, x, y + 34);
      grad.addColorStop(0, '#2b3d4f');
      grad.addColorStop(1, '#101a24');
      g.fillStyle = grad;
      g.fillRect(x, y, 30, 34);
      g.fillStyle = 'rgba(255,255,255,0.12)';
      g.fillRect(x + 2, y + 2, 10, 14);
    }
  }
}, false);

// ---------- Ground, zone, and world objects ----------
const ground = new THREE.Mesh(
  new THREE.PlaneGeometry(2000, 2000),
  new THREE.MeshStandardMaterial({ map: grassTex, roughness: 0.95, metalness: 0 })
);
ground.rotation.x = -Math.PI / 2;
ground.position.set(1000, 0, 1000);
ground.receiveShadow = true;
scene.add(ground);

// Zone: a translucent wall, built at unit radius and scaled to the live zone.
const zoneMesh = new THREE.Mesh(
  new THREE.CylinderGeometry(1, 1, 240, 128, 1, true),
  new THREE.MeshBasicMaterial({ color: 0xff3030, transparent: true, opacity: 0.18, side: THREE.DoubleSide, depthWrite: false })
);
scene.add(zoneMesh);

const boxGeo = new THREE.BoxGeometry(1, 1, 1);
const lootGeo = new THREE.BoxGeometry(10, 10, 10);
const beamGeo = new THREE.CylinderGeometry(1.5, 1.5, 220, 8, 1, true);
const matBeam = new THREE.MeshBasicMaterial({ color: 0xff5252, transparent: true, opacity: 0.6, depthWrite: false });
const matPing = new THREE.MeshBasicMaterial({ color: 0xffeb3b, transparent: true, opacity: 0.75, depthWrite: false });
const matLoot = new THREE.MeshStandardMaterial({ color: 0xf5c542, emissive: 0x553300, roughness: 0.4, metalness: 0.6 });

// Shared player materials: team colors, skin, and gear.
const matSkin = new THREE.MeshStandardMaterial({ color: 0xd9a784, roughness: 0.7 });
const matGear = new THREE.MeshStandardMaterial({ color: 0x1d1f22, roughness: 0.8 });
const matEnemyCloth = new THREE.MeshStandardMaterial({ color: 0x9b2f2f, roughness: 0.85 });
const matMateCloth = new THREE.MeshStandardMaterial({ color: 0x2f6b3a, roughness: 0.85 });

// Obstacles are deterministic from their position, so every client draws the same buildings.
const facadeCache = new Map();
function facadeFor(w, h) {
  const key = `${Math.round(w / 20)}x${Math.round(h / 20)}`;
  if (!facadeCache.has(key)) {
    const t = facadeTex.clone();
    t.wrapS = t.wrapT = THREE.RepeatWrapping;
    t.repeat.set(Math.max(1, Math.round(w / 40)), 1);
    t.needsUpdate = true;
    facadeCache.set(key, t);
  }
  return facadeCache.get(key);
}
function hash01(x, y) {
  const s = Math.sin(x * 12.9898 + y * 78.233) * 43758.5453;
  return s - Math.floor(s);
}

const playerMeshes = new Map();
const lootMeshes = new Map();
const obstacleMeshes = new Map();
const beaconMeshes = new Map();
const pingMeshes = [];
const tracers = [];

function makePlayerMesh() {
  const g = new THREE.Group();
  const torso = new THREE.Mesh(new THREE.BoxGeometry(14, 22, 10), matEnemyCloth);
  torso.position.y = 26;
  const legs = new THREE.Mesh(new THREE.BoxGeometry(12, 22, 8), matGear);
  legs.position.y = 11;
  const head = new THREE.Mesh(new THREE.SphereGeometry(6, 16, 12), matSkin);
  head.position.y = 42;
  const helmet = new THREE.Mesh(new THREE.SphereGeometry(6.6, 16, 12, 0, Math.PI * 2, 0, Math.PI / 2), matGear);
  helmet.position.y = 43;
  for (const m of [torso, legs, head, helmet]) { m.castShadow = true; m.receiveShadow = true; }
  g.add(torso, legs, head, helmet);
  g.userData.torso = torso;
  scene.add(g);
  return g;
}

// ---------- Weapon models (first-person viewmodels built from primitives) ----------
const matGunMetal = new THREE.MeshStandardMaterial({ color: 0x2a2d31, metalness: 0.85, roughness: 0.32 });
const matPolymer = new THREE.MeshStandardMaterial({ color: 0x1b1b1b, roughness: 0.7 });
const matStock = new THREE.MeshStandardMaterial({ color: 0x6d5234, roughness: 0.6 });
const matFlash = new THREE.MeshBasicMaterial({ color: 0xffd27a, transparent: true, opacity: 0.9 });

function part(geo, mat, x, y, z, rx = 0, ry = 0, rz = 0) {
  const m = new THREE.Mesh(geo, mat);
  m.position.set(x, y, z);
  m.rotation.set(rx, ry, rz);
  m.castShadow = true;
  return m;
}
const box = (w, h, d) => new THREE.BoxGeometry(w, h, d);
const cyl = (r, len) => new THREE.CylinderGeometry(r, r, len, 14).rotateX(Math.PI / 2);

const weaponBuilders = {
  Pistol: g => {
    g.add(part(box(0.05, 0.06, 0.22), matGunMetal, 0, 0, -0.02));
    g.add(part(box(0.05, 0.07, 0.14), matPolymer, 0, -0.05, 0.02));
    g.add(part(box(0.045, 0.13, 0.05), matPolymer, 0, -0.12, 0.07, 0.25));
    return { muzzle: new THREE.Vector3(0, 0.005, -0.15) };
  },
  SMG: g => {
    g.add(part(box(0.06, 0.09, 0.34), matGunMetal, 0, 0, -0.04));
    g.add(part(box(0.045, 0.2, 0.05), matPolymer, 0, -0.14, -0.04, 0.12));
    g.add(part(box(0.05, 0.07, 0.2), matPolymer, 0, -0.01, 0.2));
    g.add(part(cyl(0.012, 0.12), matGunMetal, 0, 0.005, -0.27));
    return { muzzle: new THREE.Vector3(0, 0.005, -0.34) };
  },
  Rifle: g => {
    g.add(part(box(0.06, 0.1, 0.42), matGunMetal, 0, 0, -0.04));
    g.add(part(box(0.055, 0.07, 0.22), matPolymer, 0, -0.01, -0.3));
    g.add(part(cyl(0.014, 0.3), matGunMetal, 0, 0.01, -0.52));
    g.add(part(box(0.05, 0.2, 0.08), matPolymer, 0, -0.15, -0.08, 0.12));
    g.add(part(box(0.06, 0.1, 0.28), matStock, 0, -0.01, 0.3));
    g.add(part(box(0.045, 0.12, 0.05), matPolymer, 0, -0.12, 0.1, 0.2));
    return { muzzle: new THREE.Vector3(0, 0.01, -0.68) };
  },
  Shotgun: g => {
    g.add(part(cyl(0.024, 0.6), matGunMetal, 0, 0.02, -0.4));
    g.add(part(box(0.07, 0.1, 0.3), matGunMetal, 0, -0.01, -0.02));
    g.add(part(box(0.06, 0.06, 0.16), matPolymer, 0, -0.05, -0.3));
    g.add(part(box(0.07, 0.11, 0.3), matStock, 0, -0.02, 0.28));
    g.add(part(box(0.045, 0.12, 0.05), matPolymer, 0, -0.12, 0.06, 0.25));
    return { muzzle: new THREE.Vector3(0, 0.02, -0.72) };
  },
  Sniper: g => {
    g.add(part(cyl(0.016, 0.7), matGunMetal, 0, 0.02, -0.5));
    g.add(part(box(0.06, 0.1, 0.36), matGunMetal, 0, 0, -0.04));
    g.add(part(cyl(0.03, 0.26), matGunMetal, 0, 0.09, -0.08));
    g.add(part(box(0.07, 0.12, 0.36), matStock, 0, -0.01, 0.3));
    g.add(part(box(0.045, 0.12, 0.05), matPolymer, 0, -0.12, 0.08, 0.2));
    return { muzzle: new THREE.Vector3(0, 0.02, -0.86) };
  },
};

const viewmodels = new Map();
const flashLight = new THREE.PointLight(0xffc070, 0, 6, 2);
function viewmodelFor(weaponName) {
  if (!viewmodels.has(weaponName)) {
    const g = new THREE.Group();
    const { muzzle } = weaponBuilders[weaponName](g);
    const flash = new THREE.Mesh(new THREE.SphereGeometry(0.05, 10, 8), matFlash);
    flash.position.copy(muzzle);
    flash.visible = false;
    g.add(flash);
    g.userData.flash = flash;
    g.userData.muzzle = muzzle;
    viewmodels.set(weaponName, g);
    camera.add(g);
  }
  return viewmodels.get(weaponName);
}
camera.add(flashLight);
flashLight.position.set(0.2, -0.2, -0.4);

// ---------- Overlay (minimap, kill feed, crosshair, HUD, touch controls) ----------
const overlay = document.getElementById('overlay');
const ctx = overlay.getContext('2d');
const hud = document.getElementById('status');
const STICK = { x: 110, y: 0, r: 70, hit: 90 };
const FIRE = { x: 0, y: 0, r: 55, hit: 65 };
const RELOAD = { x: 0, y: 0, r: 34, hit: 42 };
const SWAP = { x: 0, y: 0, r: 30, hit: 38 };

function resize() {
  renderer.setSize(innerWidth, innerHeight);
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
  overlay.width = innerWidth;
  overlay.height = innerHeight;
  STICK.y = innerHeight - 110;
  FIRE.x = innerWidth - 110; FIRE.y = innerHeight - 110;
  RELOAD.x = innerWidth - 230; RELOAD.y = innerHeight - 110;
  SWAP.x = innerWidth - 110; SWAP.y = innerHeight - 220;
}
addEventListener('resize', resize);
resize();

// ---------- Input ----------
const keys = new Set();
const stick = { dx: 0, dy: 0 };
let fireHeld = false;
let shootHeld = false;
let touchActive = false;
let yaw = 0;
let pendingPing = null;
let pendingReload = false;
let pendingSwitch = 0;
const pointers = new Map();

function requestSwitch(slot) { pendingSwitch = slot; }

addEventListener('keydown', e => {
  ensureAudio();
  const k = e.key.toLowerCase();
  if (e.repeat && k !== ' ') return;
  keys.add(k);
  if (k === 'm') muted = !muted;
  if (k === 'r') pendingReload = true;
  if (k >= '1' && k <= '5') requestSwitch(Number(k));
  if (k === 'q' && curr && curr.me.alive) {
    pendingPing = { x: curr.me.x + Math.cos(yaw) * 300, y: curr.me.y + Math.sin(yaw) * 300 };
  }
});
addEventListener('keyup', e => keys.delete(e.key.toLowerCase()));
overlay.addEventListener('wheel', e => {
  if (!curr) return;
  const slot = curr.ammo.slot + (e.deltaY > 0 ? 1 : -1);
  requestSwitch(((slot - 1 + 5) % 5) + 1);
}, { passive: true });

overlay.style.touchAction = 'none';
const locked = () => document.pointerLockElement === overlay;
const overlayPos = e => {
  const r = overlay.getBoundingClientRect();
  return { x: e.clientX - r.left, y: e.clientY - r.top };
};
const dist = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);
const inRect = (p, r) => p.x >= r.x && p.x <= r.x + r.w && p.y >= r.y && p.y <= r.y + r.h;
const minimapRect = { x: MINIMAP.x, y: MINIMAP.y, w: MINIMAP.size, h: MINIMAP.size };

overlay.addEventListener('pointerdown', e => {
  ensureAudio();
  overlay.setPointerCapture(e.pointerId);
  const p = overlayPos(e);
  if (e.pointerType === 'touch') touchActive = true;

  if (e.pointerType === 'touch') {
    if (inRect(p, minimapRect) && curr) {
      const k = 2000 / MINIMAP.size;
      pendingPing = { x: (p.x - MINIMAP.x) * k, y: (p.y - MINIMAP.y) * k };
      pointers.set(e.pointerId, { role: 'minimap' });
      return;
    }
    if (dist(p, STICK) <= STICK.hit) { pointers.set(e.pointerId, { role: 'stick' }); updateStick(p); return; }
    if (dist(p, FIRE) <= FIRE.hit) { fireHeld = true; pointers.set(e.pointerId, { role: 'fire' }); return; }
    if (dist(p, RELOAD) <= RELOAD.hit) { pendingReload = true; pointers.set(e.pointerId, { role: 'reload' }); return; }
    if (dist(p, SWAP) <= SWAP.hit) {
      if (curr) requestSwitch(((curr.ammo.slot) % 5) + 1);
      pointers.set(e.pointerId, { role: 'swap' });
      return;
    }
    pointers.set(e.pointerId, { role: 'look', x: p.x, y: p.y });
    return;
  }
  // Mouse: the first click captures the pointer for mouse-look; while captured, the button fires.
  if (!locked()) overlay.requestPointerLock();
  else shootHeld = true;
  pointers.set(e.pointerId, { role: 'mouse' });
});

overlay.addEventListener('pointermove', e => {
  if (e.pointerType === 'mouse' && locked()) { yaw += e.movementX * 0.0025; return; }
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
let lastReloading = false;

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
function noiseBurst(dur, vol = 0.12, cutoff = 0) {
  if (!audioCtx || muted) return;
  const n = Math.floor(audioCtx.sampleRate * dur);
  const buf = audioCtx.createBuffer(1, n, audioCtx.sampleRate);
  const data = buf.getChannelData(0);
  for (let i = 0; i < n; i++) data[i] = (Math.random() * 2 - 1) * (1 - i / n);
  const src = audioCtx.createBufferSource();
  const g = audioCtx.createGain();
  g.gain.value = vol;
  src.buffer = buf;
  let node = src;
  if (cutoff) {
    const f = audioCtx.createBiquadFilter();
    f.type = 'lowpass';
    f.frequency.value = cutoff;
    node = src.connect(f);
  }
  node.connect(g).connect(audioCtx.destination);
  src.start();
}
// Each weapon has its own voice: a sharp crack for the pistol, a chatter for the SMG, and so on.
const SHOT_SOUND = {
  Pistol: () => { noiseBurst(0.09, 0.22, 3500); tone(160, 0.08, 'sawtooth', 0.07, 55); },
  SMG: () => { noiseBurst(0.05, 0.14, 4000); tone(200, 0.05, 'square', 0.04, 90); },
  Rifle: () => { noiseBurst(0.1, 0.2, 3000); tone(130, 0.1, 'sawtooth', 0.07, 45); },
  Shotgun: () => { noiseBurst(0.22, 0.34, 1800); tone(90, 0.2, 'sawtooth', 0.12, 30); },
  Sniper: () => { noiseBurst(0.18, 0.3, 2400); tone(70, 0.3, 'sawtooth', 0.12, 25); tone(1400, 0.05, 'sine', 0.05, null, 0.12); },
};
const SFX = {
  hit: () => tone(520, 0.08, 'triangle', 0.1, 260),
  headshot: () => { tone(880, 0.12, 'sine', 0.12, 440); tone(1320, 0.1, 'sine', 0.08, null, 0.06); },
  kill: () => tone(120, 0.45, 'sawtooth', 0.15, 40),
  pickup: () => { tone(440, 0.08, 'square', 0.06); tone(660, 0.12, 'square', 0.06, null, 0.08); },
  ping: () => tone(990, 0.25, 'sine', 0.08),
  zone: () => tone(300, 0.35, 'triangle', 0.1),
  reloadStart: () => { tone(900, 0.05, 'square', 0.05); tone(600, 0.05, 'square', 0.05, null, 0.35); },
  reloadDone: () => tone(1100, 0.08, 'square', 0.06),
  switch: () => tone(700, 0.04, 'square', 0.04),
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
    if (msg.ammo.slot !== curr.ammo.slot) SFX.switch();
  }
  if (msg.ammo.reloading !== lastReloading) {
    lastReloading = msg.ammo.reloading;
    if (msg.ammo.reloading) SFX.reloadStart(); else SFX.reloadDone();
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
  if (shoot && curr && curr.me.alive && curr.ammo.mag > 0 && !curr.ammo.reloading && performance.now() - lastShotAt > 60) {
    lastShotAt = performance.now();
    onLocalShot();
  }
  let ping = false, px = 0, py = 0;
  if (pendingPing) {
    ping = true;
    px = clamp(pendingPing.x, 0, 2000);
    py = clamp(pendingPing.y, 0, 2000);
    pendingPing = null;
  }
  const reload = pendingReload;
  pendingReload = false;
  const sw = pendingSwitch;
  pendingSwitch = 0;
  ws.send(JSON.stringify({ t: 'input', dx, dy, aim: yaw, shoot, ping, px, py, reload, switch: sw }));
}, TICK_MS);

// Local shots: play the weapon's sound, kick the viewmodel, flash the muzzle, and draw a tracer.
// The server decides whether the shot actually hits; the client only shows it immediately.
let recoil = 0;
let flashUntil = 0;
function onLocalShot() {
  const w = curr.ammo.weapon;
  (SHOT_SOUND[w] ?? SHOT_SOUND.Pistol)();
  recoil = w === 'Shotgun' || w === 'Sniper' ? 1 : 0.6;
  flashUntil = performance.now() + 60;
  const vm = viewmodelFor(w);
  const muzzle = vm.userData.muzzle.clone().applyMatrix4(vm.matrixWorld);
  const range = curr.ammo.range;
  const end = new THREE.Vector3(curr.me.x + Math.cos(yaw) * range, EYE - 2, curr.me.y + Math.sin(yaw) * range);
  const geo = new THREE.BufferGeometry().setFromPoints([muzzle, end]);
  const line = new THREE.Line(geo, new THREE.LineBasicMaterial({ color: 0xfff0b0, transparent: true, opacity: 0.8 }));
  scene.add(line);
  tracers.push({ obj: line, until: performance.now() + 90 });
}

// ---------- Scene sync ----------
function syncPlayers() {
  const prevById = new Map(prev.players.map(p => [p.id, p]));
  const seen = new Set();
  for (const p of curr.players) {
    seen.add(p.id);
    let mesh = playerMeshes.get(p.id);
    if (!mesh) { mesh = makePlayerMesh(); playerMeshes.set(p.id, mesh); }
    const old = prevById.get(p.id) ?? p;
    mesh.position.set(lerp(old.x, p.x, 0.5), 0, lerp(old.y, p.y, 0.5));
    const teammate = curr.me.team !== 0 && p.team === curr.me.team;
    mesh.userData.torso.material = teammate ? matMateCloth : matEnemyCloth;
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
      const height = 50 + hash01(r.x, r.y) * 90;
      const tex = facadeFor(r.w, height);
      const mat = new THREE.MeshStandardMaterial({ map: tex, roughness: 0.85, metalness: 0 });
      const m = new THREE.Mesh(boxGeo, mat);
      m.scale.set(r.w, height, r.h);
      m.position.set(r.x + r.w / 2, height / 2, r.y + r.h / 2);
      m.castShadow = true;
      m.receiveShadow = true;
      scene.add(m);
      obstacleMeshes.set(key, m);
    }
  }
  for (const [key, m] of obstacleMeshes) {
    if (!seen.has(key)) { scene.remove(m); m.material.dispose(); obstacleMeshes.delete(key); }
  }
}

function syncLoot(now) {
  const seen = new Set();
  for (const l of curr.loot) {
    seen.add(l.id);
    let m = lootMeshes.get(l.id);
    if (!m) {
      m = new THREE.Mesh(lootGeo, matLoot);
      m.castShadow = true;
      scene.add(m);
      lootMeshes.set(l.id, m);
    }
    m.position.set(l.x, 8 + Math.sin(now / 300 + l.id) * 2, l.y);
    m.rotation.y = now / 800;
  }
  for (const [lid, m] of lootMeshes) {
    if (!seen.has(lid)) { scene.remove(m); lootMeshes.delete(lid); }
  }
}

function syncBeacons() {
  const seen = new Set();
  for (const b of curr.revealed) {
    seen.add(b.id);
    let m = beaconMeshes.get(b.id);
    if (!m) {
      m = new THREE.Mesh(beamGeo, matBeam);
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

function updateWorld(meX, meY) {
  const r = curr.zone.r;
  zoneMesh.position.set(curr.zone.x, 120, curr.zone.y);
  zoneMesh.scale.set(r, 1, r);
  // The shadow light follows the player so nearby shadows stay sharp.
  sun.position.set(meX + 380, 700, meY + 260);
  sun.target.position.set(meX, 0, meY);
  sun.target.updateMatrixWorld();
}

// ---------- Viewmodel (weapon in view) ----------
function updateViewmodel(now) {
  const name = curr.ammo.weapon;
  for (const [wName, vm] of viewmodels) vm.visible = wName === name && curr.me.alive;
  const vm = viewmodelFor(name);

  recoil = Math.max(0, recoil - 0.12);
  const reloadFrac = curr.ammo.reloading ? curr.ammo.reloadFrac : 0;
  const lower = curr.ammo.reloading ? Math.sin(reloadFrac * Math.PI) * 0.28 : 0;
  const sway = Math.sin(now / 420) * 0.004;
  vm.position.set(0.22 + sway, -0.24 - lower, -0.42 + recoil * 0.05);
  vm.rotation.set(-recoil * 0.07 + lower * 0.5, 0, 0);

  const flashing = now < flashUntil;
  vm.userData.flash.visible = flashing;
  flashLight.intensity = flashing ? 2.5 : 0;
}

// ---------- Camera ----------
function updateCamera(meX, meY) {
  camera.position.set(meX, EYE, meY);
  camera.lookAt(meX + Math.cos(yaw) * 100, EYE, meY + Math.sin(yaw) * 100);
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

function drawAmmo() {
  const a = curr.ammo;
  const x = overlay.width - 230, y = overlay.height - 190;
  ctx.textAlign = 'right';
  ctx.fillStyle = '#fff';
  ctx.font = 'bold 40px sans-serif';
  ctx.fillText(`${a.mag}`, x + 90, y + 40);
  ctx.font = '20px sans-serif';
  ctx.fillStyle = 'rgba(255,255,255,0.7)';
  ctx.fillText(`/ ${a.reserve}`, x + 150, y + 40);
  ctx.textAlign = 'left';
  ctx.fillStyle = '#ffd54f';
  ctx.font = 'bold 14px sans-serif';
  ctx.fillText(`${a.slot}  ${a.weapon.toUpperCase()}${a.auto ? ' · AUTO' : ''}`, x - 210, y + 20);
  if (a.reloading) {
    ctx.fillStyle = 'rgba(255,255,255,0.25)';
    ctx.fillRect(x - 210, y + 30, 210, 8);
    ctx.fillStyle = '#ffd54f';
    ctx.fillRect(x - 210, y + 30, 210 * a.reloadFrac, 8);
    ctx.fillStyle = '#fff';
    ctx.font = '12px sans-serif';
    ctx.fillText('RELOADING', x - 210, y + 52);
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

function drawButton(b, label, color, alpha) {
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.fillStyle = color;
  ctx.beginPath(); ctx.arc(b.x, b.y, b.r, 0, Math.PI * 2); ctx.fill();
  ctx.globalAlpha = 1;
  ctx.fillStyle = '#fff';
  ctx.font = 'bold 13px sans-serif';
  ctx.textAlign = 'center';
  ctx.fillText(label, b.x, b.y + 5);
  ctx.restore();
}

function drawTouchControls() {
  ctx.save();
  ctx.globalAlpha = 0.35;
  ctx.fillStyle = '#000';
  ctx.beginPath(); ctx.arc(STICK.x, STICK.y, STICK.r, 0, Math.PI * 2); ctx.fill();
  ctx.fillStyle = '#fff';
  ctx.beginPath(); ctx.arc(STICK.x + stick.dx * STICK.r, STICK.y + stick.dy * STICK.r, 26, 0, Math.PI * 2); ctx.fill();
  ctx.restore();
  drawButton(FIRE, 'FIRE', '#ef5350', fireHeld ? 0.7 : 0.35);
  drawButton({ x: RELOAD.x, y: RELOAD.y, r: RELOAD.r }, 'R', '#ffb300', 0.45);
  drawButton({ x: SWAP.x, y: SWAP.y, r: SWAP.r }, 'SWAP', '#7e57c2', 0.45);
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
    drawAmmo();
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

  for (let i = tracers.length - 1; i >= 0; i--) {
    if (now > tracers[i].until) {
      scene.remove(tracers[i].obj);
      tracers[i].obj.geometry.dispose();
      tracers.splice(i, 1);
    }
  }

  if (curr) {
    const t = Math.min(1, (now - currAt) / TICK_MS);
    const meX = lerp(prev.me.x, curr.me.x, t);
    const meY = lerp(prev.me.y, curr.me.y, t);
    syncPlayers();
    syncObstacles();
    syncLoot(now);
    syncBeacons();
    syncPings();
    updateWorld(meX, meY);
    updateCamera(meX, meY);
    updateViewmodel(now);
  }

  renderer.render(scene, camera);
  drawOverlay(now);
}
requestAnimationFrame(frame);
