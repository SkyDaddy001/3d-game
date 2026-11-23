import * as THREE from 'three';
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js';
import { RoundedBoxGeometry } from 'three/addons/geometries/RoundedBoxGeometry.js';
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js';
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js';

// World units match the server: x and y span 0..2000 on the ground plane.
// Three.js uses Y as up, so server (x, y) maps to scene (x, height, z = y).
const { id } = window.MATCH;
const EYE = 30, CROUCH_EYE = 16;                 // must match height.go
const JUMP_SPEED = 78, GRAVITY = 220, CROUCH_SPEED = 0.55, MAX_PITCH = 1.2;
const SWIM_SPEED = 0.5, SWIM_EYE = 8, BLAST_RADIUS = 95; // terrain.go and gear.go
const TICK_MS = 1000 / 30;
const REPLAY = !!window.MATCH.replay;                              // spectating a recording
const CAPTURE = new URLSearchParams(location.search).has('capture'); // frames are stepped from outside
let vclock = 0;                                                      // animation clock while replaying
const lerp = (a, b, t) => a + (b - a) * t;
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
// Phones get a lighter pipeline: no bloom and smaller shadows.
const LOW = matchMedia('(pointer: coarse)').matches;
const DPR = Math.min(devicePixelRatio || 1, LOW ? 1.5 : 2);

// Deterministic random numbers, so every client decorates the world the same way.
function hash01(x, y) {
  const s = Math.sin(x * 12.9898 + y * 78.233) * 43758.5453;
  return s - Math.floor(s);
}
function hashStr(str) {
  let h = 2166136261;
  for (let i = 0; i < str.length; i++) h = Math.imul(h ^ str.charCodeAt(i), 16777619);
  return (h >>> 0) / 4294967296;
}

// ---------- Renderer, lighting, and sky ----------
const renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' });
renderer.setPixelRatio(DPR);
renderer.setSize(innerWidth, innerHeight);
renderer.outputColorSpace = THREE.SRGBColorSpace;
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.2;
renderer.shadowMap.enabled = true;
renderer.shadowMap.type = THREE.PCFSoftShadowMap;
document.getElementById('scene-root').appendChild(renderer.domElement);

const scene = new THREE.Scene();
const pmrem = new THREE.PMREMGenerator(renderer);
scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
const HORIZON = new THREE.Color(0xd4ecff);
scene.fog = new THREE.Fog(HORIZON, 700, 3000);
scene.background = HORIZON;

const camera = new THREE.PerspectiveCamera(72, innerWidth / innerHeight, 0.5, 6000);
scene.add(camera);

// A warm key light with a cool sky fill: the classic animated-film outdoor setup.
const SUN_DIR = new THREE.Vector3(0.55, 0.72, 0.38).normalize();
const hemi = new THREE.HemisphereLight(0xcfe8ff, 0x8fbf6a, 1.0);
scene.add(hemi);
const sun = new THREE.DirectionalLight(0xffe4b8, 3.2);
sun.castShadow = true;
sun.shadow.mapSize.set(LOW ? 1024 : 2048, LOW ? 1024 : 2048);
Object.assign(sun.shadow.camera, { left: -320, right: 320, top: 320, bottom: -320, near: 10, far: 2000 });
sun.shadow.bias = -0.0006;
sun.shadow.normalBias = 0.8;
sun.shadow.radius = 4;
scene.add(sun, sun.target);

// Sky dome: a gradient from a deep zenith to a pale horizon, with a soft sun glow.
const skyDome = new THREE.Mesh(
  new THREE.SphereGeometry(4500, 32, 16),
  new THREE.ShaderMaterial({
    side: THREE.BackSide,
    depthWrite: false,
    fog: false,
    uniforms: {
      zenith: { value: new THREE.Color(0x2f7fe0) },
      horizon: { value: HORIZON },
      sunDir: { value: SUN_DIR },
    },
    vertexShader: `
      varying vec3 vDir;
      void main() {
        vDir = normalize(position);
        gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
      }`,
    fragmentShader: `
      uniform vec3 zenith; uniform vec3 horizon; uniform vec3 sunDir;
      varying vec3 vDir;
      void main() {
        vec3 d = normalize(vDir);
        float h = clamp(d.y, 0.0, 1.0);
        vec3 col = mix(horizon, zenith, pow(h, 0.55));
        float s = max(dot(d, sunDir), 0.0);
        col += vec3(1.0, 0.85, 0.6) * (pow(s, 6.0) * 0.25 + pow(s, 400.0) * 6.0);
        gl_FragColor = vec4(col, 1.0);
      }`,
  })
);
skyDome.renderOrder = -10;
scene.add(skyDome);

// Clouds: clusters of soft white puffs that drift slowly.
const clouds = new THREE.Group();
{
  const puffGeo = new THREE.SphereGeometry(1, 14, 10);
  const puffMat = new THREE.MeshStandardMaterial({ color: 0xffffff, roughness: 1, emissive: 0xbfd4ea, emissiveIntensity: 0.55, fog: false });
  for (let i = 0; i < 16; i++) {
    const cloud = new THREE.Group();
    const puffs = 4 + Math.floor(hash01(i, 1) * 4);
    for (let j = 0; j < puffs; j++) {
      const m = new THREE.Mesh(puffGeo, puffMat);
      const r = 45 + hash01(i, j + 2) * 55;
      m.scale.set(r * 1.5, r * 0.75, r);
      m.position.set((j - puffs / 2) * 60 + hash01(j, i) * 30, hash01(i + j, 7) * 22, hash01(j, i + 9) * 60 - 30);
      cloud.add(m);
    }
    cloud.position.set(-1500 + hash01(i, 3) * 5000, 520 + hash01(i, 4) * 260, -1500 + hash01(i, 5) * 5000);
    clouds.add(cloud);
  }
  scene.add(clouds);
}

// Post-processing: a gentle bloom on bright highlights, then tone mapping.
let composer = null;
if (!LOW) {
  try {
    const target = new THREE.WebGLRenderTarget(innerWidth * DPR, innerHeight * DPR, { type: THREE.HalfFloatType, samples: 4 });
    composer = new EffectComposer(renderer, target);
    composer.addPass(new RenderPass(scene, camera));
    composer.addPass(new UnrealBloomPass(new THREE.Vector2(innerWidth, innerHeight), 0.22, 0.7, 0.92));
    composer.addPass(new OutputPass());
  } catch (err) {
    console.warn('post-processing unavailable, rendering directly', err);
    composer = null;
  }
}

// ---------- Procedural textures (no image files needed) ----------
function makeCanvasTexture(repeat, draw, tile = true, size = 256) {
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

// Painterly grass: soft color blotches with short brush strokes, drawn to wrap at the edges.
const grassTex = makeCanvasTexture(28, (g, w, h) => {
  g.fillStyle = '#69c245';
  g.fillRect(0, 0, w, h);
  const blotch = (x, y, r, color) => {
    for (const ox of [-w, 0, w]) for (const oy of [-h, 0, h]) {
      const grad = g.createRadialGradient(x + ox, y + oy, 0, x + ox, y + oy, r);
      grad.addColorStop(0, color);
      grad.addColorStop(1, 'rgba(0,0,0,0)');
      g.fillStyle = grad;
      g.fillRect(x + ox - r, y + oy - r, r * 2, r * 2);
    }
  };
  for (let i = 0; i < 26; i++) {
    blotch(hash01(i, 11) * w, hash01(i, 12) * h, 50 + hash01(i, 13) * 70,
      ['rgba(140,214,84,0.55)', 'rgba(72,170,60,0.5)', 'rgba(190,226,104,0.35)'][i % 3]);
  }
  g.lineCap = 'round';
  for (let i = 0; i < 420; i++) {
    const x = hash01(i, 21) * w, y = hash01(i, 22) * h;
    g.strokeStyle = i % 2 ? 'rgba(48,140,52,0.35)' : 'rgba(205,240,130,0.35)';
    g.lineWidth = 2;
    g.beginPath();
    g.moveTo(x, y);
    g.lineTo(x + (hash01(i, 23) - 0.5) * 6, y - 5 - hash01(i, 24) * 6);
    g.stroke();
  }
}, true, 512);

// Building wall tile: plaster with two columns and three rows of glass windows. The plaster is
// near-white so each building's own color tints it.
const WALL_TILE_W = 44, WALL_TILE_H = 48;
const wallTex = makeCanvasTexture(1, (g, w, h) => {
  g.fillStyle = '#f6f1e7';
  g.fillRect(0, 0, w, h);
  for (let i = 0; i < 300; i++) {
    g.fillStyle = i % 2 ? 'rgba(255,255,255,0.25)' : 'rgba(120,105,90,0.07)';
    g.fillRect(hash01(i, 31) * w, hash01(i, 32) * h, 6, 3);
  }
  const cols = 2, rows = 3, cw = w / cols, rh = h / rows;
  for (let r = 0; r < rows; r++) for (let c = 0; c < cols; c++) {
    const x = c * cw + cw * 0.22, y = r * rh + rh * 0.2, ww = cw * 0.56, wh = rh * 0.58;
    g.fillStyle = '#fffdf8';
    g.fillRect(x - 5, y - 5, ww + 10, wh + 10);
    const lit = hash01(r * 7 + c, 33) > 0.72;
    const grad = g.createLinearGradient(x, y, x + ww, y + wh);
    if (lit) { grad.addColorStop(0, '#ffe9a6'); grad.addColorStop(1, '#ffc35c'); }
    else { grad.addColorStop(0, '#9ed4f5'); grad.addColorStop(1, '#3f7fb5'); }
    g.fillStyle = grad;
    g.fillRect(x, y, ww, wh);
    g.fillStyle = 'rgba(255,255,255,0.45)';
    g.beginPath();
    g.moveTo(x + ww * 0.1, y + wh); g.lineTo(x + ww * 0.45, y); g.lineTo(x + ww * 0.7, y); g.lineTo(x + ww * 0.35, y + wh);
    g.fill();
    g.fillStyle = '#fffdf8';
    g.fillRect(x + ww / 2 - 2, y, 4, wh);
    g.fillStyle = 'rgba(90,70,60,0.25)';
    g.fillRect(x - 5, y + wh + 5, ww + 10, 4);
  }
}, true, 512);

// Medkit: a white box with a red cross on each side.
const medkitTex = makeCanvasTexture(1, (g, w, h) => {
  g.fillStyle = '#ffffff';
  g.fillRect(0, 0, w, h);
  g.fillStyle = '#e53935';
  g.fillRect(w * 0.4, h * 0.18, w * 0.2, h * 0.64);
  g.fillRect(w * 0.18, h * 0.4, w * 0.64, h * 0.2);
}, false, 128);

// ---------- Ground, zone, and world objects ----------
const ground = new THREE.Mesh(
  new THREE.PlaneGeometry(6000, 6000),
  new THREE.MeshStandardMaterial({ map: grassTex, roughness: 1, metalness: 0 })
);
grassTex.repeat.set(84, 84);
ground.rotation.x = -Math.PI / 2;
ground.position.set(1000, 0, 1000);
ground.receiveShadow = true;
scene.add(ground);

// Zone: a translucent wall, built at unit radius and scaled to the live zone.
const zoneMesh = new THREE.Mesh(
  new THREE.CylinderGeometry(1, 1, 300, 128, 1, true),
  new THREE.MeshBasicMaterial({ color: 0xff4a6a, transparent: true, opacity: 0.2, side: THREE.DoubleSide, depthWrite: false, fog: false })
);
scene.add(zoneMesh);

const boxGeo = new THREE.BoxGeometry(1, 1, 1);
const lootGeo = new RoundedBoxGeometry(11, 9, 11, 3, 2);
const beamGeo = new THREE.CylinderGeometry(1.5, 1.5, 220, 8, 1, true);
const coinGeo = new THREE.CylinderGeometry(6, 6, 1.6, 24).rotateX(Math.PI / 2);
const matCoin = new THREE.MeshStandardMaterial({ color: 0xffc933, metalness: 0.9, roughness: 0.25, emissive: 0xaa6a00, emissiveIntensity: 0.5 });
const matBeam = new THREE.MeshBasicMaterial({ color: 0xff5252, transparent: true, opacity: 0.6, depthWrite: false });
const matPing = new THREE.MeshBasicMaterial({ color: 0xffeb3b, transparent: true, opacity: 0.75, depthWrite: false });
const matLoot = new THREE.MeshStandardMaterial({ map: medkitTex, roughness: 0.45, emissive: 0xffffff, emissiveMap: medkitTex, emissiveIntensity: 0.25 });

// Buildings: pastel walls tinted per building, a roof slab, and a base trim.
const BUILDING_COLORS = [0xffd9a0, 0xffb7a8, 0xbfe3ff, 0xd6f0b8, 0xf3d1ff, 0xfff1b8, 0xffcfd8, 0xc8f0e4];
const ROOF_COLORS = [0xd9603b, 0x4f7fd1, 0x5aa66a, 0x8e5bb5, 0xe0933a];
const wallTexCache = new Map();
function wallTexFor(width, height) {
  const rx = Math.max(1, Math.round(width / WALL_TILE_W)), ry = Math.max(1, Math.round(height / WALL_TILE_H));
  const key = rx + 'x' + ry;
  if (!wallTexCache.has(key)) {
    const t = wallTex.clone();
    t.repeat.set(rx, ry);
    t.needsUpdate = true;
    wallTexCache.set(key, t);
  }
  return wallTexCache.get(key);
}
function makeBuilding(r) {
  const g = new THREE.Group();
  const height = 60 + Math.round(hash01(r.x, r.y) * 3) * WALL_TILE_H * 0.75;
  const color = BUILDING_COLORS[Math.floor(hash01(r.y, r.x) * BUILDING_COLORS.length)];
  const roofColor = ROOF_COLORS[Math.floor(hash01(r.x + 1, r.y + 2) * ROOF_COLORS.length)];
  const wall = (span) => new THREE.MeshStandardMaterial({ map: wallTexFor(span, height), color, roughness: 0.85 });
  const roofMat = new THREE.MeshStandardMaterial({ color: roofColor, roughness: 0.7 });
  const sideX = wall(r.h), sideZ = wall(r.w);
  const body = new THREE.Mesh(boxGeo, [sideX, sideX, roofMat, roofMat, sideZ, sideZ]);
  body.scale.set(r.w, height, r.h);
  body.position.y = height / 2;
  const roof = new THREE.Mesh(boxGeo, roofMat);
  roof.scale.set(r.w + 6, 5, r.h + 6);
  roof.position.y = height + 2.5;
  const cap = new THREE.Mesh(boxGeo, roofMat);
  cap.scale.set(r.w * 0.45, 9, r.h * 0.45);
  cap.position.set(r.w * 0.12, height + 9, -r.h * 0.1);
  const trim = new THREE.Mesh(boxGeo, new THREE.MeshStandardMaterial({ color: 0xfffaf0, roughness: 0.9 }));
  trim.scale.set(r.w + 3, 5, r.h + 3);
  trim.position.y = 2.5;
  for (const m of [body, roof, cap, trim]) { m.castShadow = true; m.receiveShadow = true; g.add(m); }
  g.position.set(r.x + r.w / 2, 0, r.y + r.h / 2);
  return g;
}

// Crates: wooden boxes that can be shot apart.
const crateTex = makeCanvasTexture(1, (g, w, h) => {
  g.fillStyle = '#c88a48';
  g.fillRect(0, 0, w, h);
  for (let i = 0; i < 5; i++) {
    g.fillStyle = i % 2 ? 'rgba(120,70,30,0.18)' : 'rgba(255,220,160,0.14)';
    g.fillRect(0, (i * h) / 5, w, h / 5 - 3);
  }
  g.strokeStyle = '#7a4a22';
  g.lineWidth = 22;
  g.strokeRect(11, 11, w - 22, h - 22);
  g.lineWidth = 16;
  g.beginPath(); g.moveTo(16, 16); g.lineTo(w - 16, h - 16); g.stroke();
}, false, 128);
const crateGeo = new RoundedBoxGeometry(1, 1, 1, 2, 0.06);
const matCrate = new THREE.MeshStandardMaterial({ map: crateTex, roughness: 0.8 });
// Shipping containers: ribbed steel boxes in a few paint colours. They cannot be broken.
const containerTex = makeCanvasTexture(1, (g, w, h) => {
  g.fillStyle = '#ffffff';
  g.fillRect(0, 0, w, h);
  for (let x = 0; x < w; x += 16) {
    const grad = g.createLinearGradient(x, 0, x + 16, 0);
    grad.addColorStop(0, 'rgba(0,0,0,0.22)');
    grad.addColorStop(0.5, 'rgba(255,255,255,0.1)');
    grad.addColorStop(1, 'rgba(0,0,0,0.22)');
    g.fillStyle = grad;
    g.fillRect(x, 0, 16, h);
  }
  g.fillStyle = 'rgba(0,0,0,0.25)';
  g.fillRect(0, 0, w, 10);
  g.fillRect(0, h - 10, w, 10);
}, true, 256);
const CONTAINER_COLORS = [0x2f7fd6, 0xd6542f, 0x2fa56a, 0xe0a52a, 0x8a5bd6];
const containerMats = CONTAINER_COLORS.map(color => {
  const long = containerTex.clone(); long.repeat.set(3, 1); long.needsUpdate = true;
  const end = containerTex.clone(); end.repeat.set(1, 1); end.needsUpdate = true;
  return {
    long: new THREE.MeshStandardMaterial({ map: long, color, roughness: 0.55, metalness: 0.35 }),
    end: new THREE.MeshStandardMaterial({ map: end, color, roughness: 0.55, metalness: 0.35 }),
    top: new THREE.MeshStandardMaterial({ color, roughness: 0.6, metalness: 0.3 }),
  };
});
const CONTAINER_H = 38;
function makeContainer(r) {
  const c = containerMats[Math.floor(hash01(r.x, r.y) * containerMats.length)];
  // The long sides get the repeated ribbing whichever way the container lies.
  const sideX = r.h > r.w ? c.long : c.end, sideZ = r.w >= r.h ? c.long : c.end;
  const m = new THREE.Mesh(boxGeo, [sideX, sideX, c.top, c.top, sideZ, sideZ]);
  m.scale.set(r.w, CONTAINER_H, r.h);
  m.position.set(r.x + r.w / 2, CONTAINER_H / 2, r.y + r.h / 2);
  m.castShadow = true;
  m.receiveShadow = true;
  return m;
}

function makeCrate(r) {
  const m = new THREE.Mesh(crateGeo, matCrate);
  const tall = r.w + 8; // matches the server: a crate is a little taller than it is wide
  m.scale.set(r.w, tall, r.h);
  m.position.set(r.x + r.w / 2, tall / 2, r.y + r.h / 2);
  m.rotation.y = 0;
  m.castShadow = true;
  m.receiveShadow = true;
  m.userData.crate = true;
  return m;
}

// Trees: a trunk with a canopy of three soft blobs. Size and tint vary per tree.
// The trunk is tall enough that the leaves stay above a player's eyes, even right beside the tree.
const trunkGeo = new THREE.CylinderGeometry(3, 4.5, 48, 10);
const leafGeo = new THREE.IcosahedronGeometry(1, 2);
const matTrunk = new THREE.MeshStandardMaterial({ color: 0x9a6a3f, roughness: 0.9 });
const LEAF_MATS = [0x4fbf4a, 0x66cc55, 0x3fae5a, 0x8bd450].map(c => new THREE.MeshStandardMaterial({ color: c, roughness: 0.85 }));
function makeTree(r) {
  const g = new THREE.Group();
  const k = 0.85 + hash01(r.x, r.y) * 0.6;
  const mat = LEAF_MATS[Math.floor(hash01(r.y, r.x) * LEAF_MATS.length)];
  const trunk = new THREE.Mesh(trunkGeo, matTrunk);
  trunk.position.y = 24;
  g.add(trunk);
  for (const [x, y, z, s] of [[0, 64, 0, 22], [-13, 54, 6, 15], [12, 56, -7, 16], [2, 53, 13, 13]]) {
    const leaf = new THREE.Mesh(leafGeo, mat);
    leaf.scale.set(s, s * 0.9, s);
    leaf.position.set(x, y, z);
    g.add(leaf);
  }
  g.traverse(o => { if (o.isMesh) { o.castShadow = true; o.receiveShadow = true; } });
  g.scale.setScalar(k);
  g.rotation.y = hash01(r.x, 5) * Math.PI * 2;
  g.position.set(r.x + r.w / 2, 0, r.y + r.h / 2);
  return g;
}

// ---------- Terrain: lakes, bushes, grass, flowers ----------
// Lakes and bushes come from the server once. Grass and flowers are decoration, placed from fixed
// random numbers so every player sees the same field, and left out of the water.
let world = { lakes: [], bushes: [] };
const inWaterAt = (x, y) => world.lakes.some(l => Math.hypot(x - l.x, y - l.y) < l.r);
const matWater = new THREE.MeshStandardMaterial({ color: 0x2f9be6, roughness: 0.12, metalness: 0.35, transparent: true, opacity: 0.86 });
const matLakeBed = new THREE.MeshStandardMaterial({ color: 0x1d5f8f, roughness: 1 });
const matSand = new THREE.MeshStandardMaterial({ color: 0xead9a2, roughness: 1 });
const waterMeshes = [];
function buildWorld(w) {
  world = w;
  const flat = m => { m.rotation.x = -Math.PI / 2; m.receiveShadow = true; scene.add(m); return m; };
  for (const l of w.lakes) {
    flat(new THREE.Mesh(new THREE.RingGeometry(l.r - 2, l.r + 9, 56), matSand)).position.set(l.x, 0.25, l.y);
    flat(new THREE.Mesh(new THREE.CircleGeometry(l.r, 56), matLakeBed)).position.set(l.x, 0.35, l.y);
    const water = flat(new THREE.Mesh(new THREE.CircleGeometry(l.r, 56), matWater));
    water.position.set(l.x, 2.2, l.y);
    waterMeshes.push(water);
  }
  // Bushes: big enough to stand in. A player inside one is hidden from a distance.
  const blob = new THREE.IcosahedronGeometry(1, 1);
  for (const b of w.bushes) {
    const g = new THREE.Group();
    const mat = LEAF_MATS[Math.floor(hash01(b.x, b.y) * LEAF_MATS.length)];
    for (let k = 0; k < 5; k++) {
      const a = (k / 5) * Math.PI * 2 + hash01(b.x, k), d = k === 0 ? 0 : b.r * 0.55;
      const m = new THREE.Mesh(blob, mat);
      const sz = b.r * (k === 0 ? 0.8 : 0.55 + hash01(b.y, k) * 0.2);
      m.scale.set(sz, sz * 0.8, sz);
      m.position.set(Math.cos(a) * d, sz * 0.6, Math.sin(a) * d);
      m.castShadow = true;
      m.receiveShadow = true;
      g.add(m);
    }
    g.position.set(b.x, 0, b.y);
    scene.add(g);
  }
  const dummy = new THREE.Object3D();
  const place = (mesh, i, x, y, sx, sy, height) => {
    dummy.position.set(x, height, y);
    dummy.rotation.y = hash01(x, y) * 6.28;
    const k = inWaterAt(x, y) ? 0 : 1;
    dummy.scale.set(sx * k, sy * k, sx * k);
    dummy.updateMatrix();
    mesh.setMatrixAt(i, dummy.matrix);
  };
  // Grass tufts: thin blades in three greens.
  const tuftGeo = new THREE.ConeGeometry(1.3, 1, 4);
  const perShade = LOW ? 600 : 2200;
  [0x4fae3c, 0x76c94a, 0x9bd85e].forEach((color, c) => {
    const mesh = new THREE.InstancedMesh(tuftGeo, new THREE.MeshStandardMaterial({ color, roughness: 1 }), perShade);
    for (let i = 0; i < perShade; i++) {
      const h = 4 + hash01(i, 71 + c) * 6;
      place(mesh, i, hash01(i, 72 + c) * 2000, hash01(i, 75 + c) * 2000, 1, h, h / 2);
    }
    mesh.receiveShadow = true;
    scene.add(mesh);
  });
  const flowerGeo = new THREE.SphereGeometry(1.6, 8, 6);
  [0xffffff, 0xffe04a, 0xff7fb0, 0xb48cff].forEach((color, f) => {
    const mesh = new THREE.InstancedMesh(flowerGeo, new THREE.MeshStandardMaterial({ color, roughness: 0.6, emissive: color, emissiveIntensity: 0.12 }), 260);
    for (let i = 0; i < 260; i++) place(mesh, i, hash01(i, 40 + f) * 2000, hash01(i, 50 + f) * 2000, 1, 0.7, 1.6);
    scene.add(mesh);
  });
}

const playerMeshes = new Map();
const lootMeshes = new Map();
const obstacleMeshes = new Map();
const beaconMeshes = new Map();
const pingMeshes = [];
const tracers = [];

// ---------- Characters ----------
// Stylized avatars: a big head with blinking eyes, pivoted limbs for a run cycle, and colors
// picked from the player's name so each player always looks the same to everyone.
const SKINS = [0xffd2ae, 0xf2b48a, 0xc98a5e, 0x8d5a3b];
const HAIRS = [0x3b2a20, 0x6b3f22, 0xd9a13a, 0xb8432e, 0x22232b, 0x7a5cc9];
// Shirt colours, in the same order as the server's list (progress.go). The last three unlock by level.
const SHIRTS = [0xe5483f, 0xf08a2e, 0xb04fd0, 0xe04f8a, 0x3f7fe0, 0xf5c518, 0x1d2233, 0x39ffb0];
const MATE_SHIRT = 0x3fbf5f;
const matCache = new Map();
function flatMat(color, roughness = 0.6) {
  const key = color + ':' + roughness;
  if (!matCache.has(key)) matCache.set(key, new THREE.MeshStandardMaterial({ color, roughness }));
  return matCache.get(key);
}
const pick = (list, h) => list[Math.floor(h * list.length) % list.length];
const G = {
  head: new RoundedBoxGeometry(13, 12, 12, 4, 3.2),
  hair: new RoundedBoxGeometry(13.8, 6, 12.8, 4, 2.6),
  hairBack: new RoundedBoxGeometry(13.8, 9, 4, 3, 1.8),
  torso: new RoundedBoxGeometry(12, 12, 7.5, 4, 2.4),
  arm: new RoundedBoxGeometry(4.2, 11, 4.2, 3, 1.6),
  leg: new RoundedBoxGeometry(5, 10, 5.2, 3, 1.6),
  shoe: new RoundedBoxGeometry(5.6, 3.2, 8, 3, 1.4),
  hand: new THREE.SphereGeometry(2.5, 12, 10),
  eye: new THREE.SphereGeometry(1, 16, 12),
  smile: new THREE.TorusGeometry(2.1, 0.42, 8, 14, Math.PI),
  cheek: new THREE.CircleGeometry(1.4, 16),
  gun: new RoundedBoxGeometry(2.2, 9, 3.2, 2, 0.8),
};
const matEyeWhite = new THREE.MeshStandardMaterial({ color: 0xffffff, roughness: 0.25 });
const matPupil = new THREE.MeshStandardMaterial({ color: 0x1c1c24, roughness: 0.2 });
const matMouth = new THREE.MeshStandardMaterial({ color: 0x7a2e2e, roughness: 0.6 });
const matCheek = new THREE.MeshBasicMaterial({ color: 0xff8f9f, transparent: true, opacity: 0.45 });
const matShoe = flatMat(0x2e3140, 0.7);
const matToyGun = new THREE.MeshStandardMaterial({ color: 0x3a3f4a, roughness: 0.4, metalness: 0.5 });

const mateMarkerGeo = new THREE.ConeGeometry(4, 8, 4).rotateX(Math.PI);
const matMateMarker = new THREE.MeshBasicMaterial({ color: 0x5dff8f, depthTest: false, transparent: true, opacity: 0.9, fog: false });

function nameTag(name, color) {
  const c = document.createElement('canvas');
  c.width = 512; c.height = 128;
  const g = c.getContext('2d');
  g.font = '800 64px ui-rounded, system-ui, sans-serif';
  g.textAlign = 'center';
  g.lineJoin = 'round';
  g.lineWidth = 14;
  g.strokeStyle = 'rgba(20,24,40,0.85)';
  g.strokeText(name, 256, 86);
  g.fillStyle = color;
  g.fillText(name, 256, 86);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  const sprite = new THREE.Sprite(new THREE.SpriteMaterial({ map: t, depthTest: false, fog: false }));
  sprite.scale.set(48, 12, 1);
  sprite.position.y = 47;
  sprite.renderOrder = 5;
  return sprite;
}

function makePlayerMesh(p) {
  const h = hashStr(p.name);
  // A chosen look comes as "hair.skin.shirt.hat"; without one, colours come from the name.
  const look = p.look ? p.look.split('.').map(Number) : null;
  const skin = flatMat(look ? SKINS[look[1]] ?? SKINS[0] : pick(SKINS, h), 0.55);
  const hair = flatMat(look ? HAIRS[look[0]] ?? HAIRS[0] : pick(HAIRS, (h * 7) % 1), 0.5);
  const pants = flatMat(pick([0x34497a, 0x4a4f5c, 0x6a4b33], (h * 13) % 1), 0.7);
  const shirt = new THREE.MeshStandardMaterial({ color: look ? SHIRTS[look[2]] ?? SHIRTS[0] : pick(SHIRTS.slice(0, 5), (h * 3) % 1), roughness: 0.55 });

  const root = new THREE.Group(); // world position and facing
  const body = new THREE.Group(); // lean, bounce, and the downed pose
  root.add(body);
  const mesh = (geo, mat, x, y, z, parent = body) => {
    const m = new THREE.Mesh(geo, mat);
    m.position.set(x, y, z);
    parent.add(m);
    return m;
  };
  const limb = (x, y, geo, mat, len) => {
    const pivot = new THREE.Group();
    pivot.position.set(x, y, 0);
    body.add(pivot);
    mesh(geo, mat, 0, -len / 2, 0, pivot);
    return pivot;
  };
  const legL = limb(-3.1, 10, G.leg, pants, 10), legR = limb(3.1, 10, G.leg, pants, 10);
  mesh(G.shoe, matShoe, 0, -9.4, 1.2, legL);
  mesh(G.shoe, matShoe, 0, -9.4, 1.2, legR);
  const torso = mesh(G.torso, shirt, 0, 16, 0);
  const armL = limb(-8.2, 21, G.arm, shirt, 11), armR = limb(8.2, 21, G.arm, shirt, 11);
  mesh(G.hand, skin, 0, -11.5, 0, armL);
  mesh(G.hand, skin, 0, -11.5, 0, armR);
  mesh(G.gun, matToyGun, 0, -16, 1.2, armR);

  const head = new THREE.Group();
  head.position.set(0, 29, 0);
  body.add(head);
  mesh(G.head, skin, 0, 0, 0, head);
  mesh(G.hair, hair, 0, 4.2, -0.3, head);
  mesh(G.hairBack, hair, 0, 0.5, -4.6, head);
  const eyes = [];
  for (const sx of [-3.1, 3.1]) {
    const eye = new THREE.Group();
    eye.position.set(sx, 0.6, 5.7);
    const white = mesh(G.eye, matEyeWhite, 0, 0, 0, eye);
    white.scale.set(2.1, 2.5, 1);
    const pupil = mesh(G.eye, matPupil, 0, -0.2, 0.75, eye);
    pupil.scale.set(1.15, 1.4, 0.5);
    const glint = mesh(G.eye, matEyeWhite, 0.45, 0.5, 1.15, eye);
    glint.scale.setScalar(0.34);
    head.add(eye);
    eyes.push(eye);
  }
  const smile = mesh(G.smile, matMouth, 0, -2.6, 6.05, head);
  smile.rotation.z = Math.PI;
  mesh(G.cheek, matCheek, -4.7, -1.9, 6.08, head);
  mesh(G.cheek, matCheek, 4.7, -1.9, 6.08, head);
  if (look) addHat(head, look[3], mesh);

  root.traverse(o => { if (o.isMesh) { o.castShadow = true; o.receiveShadow = true; } });
  const tag = nameTag(p.name, '#ffffff');
  root.add(tag);
  // A green pointer that floats over teammates and shows through walls.
  const marker = new THREE.Mesh(mateMarkerGeo, matMateMarker);
  marker.position.y = 58;
  marker.renderOrder = 6;
  marker.visible = false;
  root.add(marker);
  root.userData = {
    body, head, torso, armL, armR, legL, legR, eyes, shirt, tag, marker, name: p.name,
    enemyColor: shirt.color.getHex(), mate: false, phase: h * 10, down: 0,
    blinkAt: performance.now() + 1500 + h * 3000, facing: null,
  };
  scene.add(root);
  return root;
}

// Hats, by index in the server's hat list: 1 cap, 2 headband, 3 crown, 4 halo.
const HAT = {
  cap: new RoundedBoxGeometry(14.4, 4.6, 13.4, 3, 2),
  brim: new RoundedBoxGeometry(11, 1.2, 7, 2, 0.5),
  band: new RoundedBoxGeometry(13.7, 2.4, 12.7, 2, 0.8),
  ring: new THREE.CylinderGeometry(5.6, 5.6, 3.2, 20, 1, true),
  spike: new THREE.ConeGeometry(1.5, 3.6, 4),
  halo: new THREE.TorusGeometry(5.4, 0.7, 10, 28).rotateX(Math.PI / 2),
};
const matCap = flatMat(0x2f6fd6, 0.6);
const matBand = flatMat(0xe5483f, 0.6);
const matCrown = new THREE.MeshStandardMaterial({ color: 0xf5c518, metalness: 0.9, roughness: 0.25, side: THREE.DoubleSide });
const matHalo = new THREE.MeshStandardMaterial({ color: 0xfff2a0, emissive: 0xffe873, emissiveIntensity: 2.2 });
function addHat(head, kind, mesh) {
  if (kind === 1) {
    mesh(HAT.cap, matCap, 0, 6.4, -0.2, head);
    mesh(HAT.brim, matCap, 0, 4.9, 8.2, head);
  } else if (kind === 2) {
    mesh(HAT.band, matBand, 0, 2.6, -0.2, head);
  } else if (kind === 3) {
    mesh(HAT.ring, matCrown, 0, 8.6, 0, head);
    for (let i = 0; i < 6; i++) {
      const a = (i / 6) * Math.PI * 2;
      mesh(HAT.spike, matCrown, Math.cos(a) * 5.4, 11.6, Math.sin(a) * 5.4, head);
    }
  } else if (kind === 4) {
    mesh(HAT.halo, matHalo, 0, 12.5, 0, head);
  }
}

function disposeAvatar(root) {
  scene.remove(root);
  root.userData.shirt.dispose();
  root.userData.tag.material.map.dispose();
  root.userData.tag.material.dispose();
}

// animateAvatar poses one character for this frame. speed is in world units per second.
function animateAvatar(root, p, speed, dt, now, mate) {
  const u = root.userData;
  if (u.mate !== mate) {
    u.mate = mate;
    u.shirt.color.setHex(mate ? MATE_SHIRT : u.enemyColor);
    root.remove(u.tag);
    u.tag.material.map.dispose();
    u.tag.material.dispose();
    u.tag = nameTag(u.name, mate ? '#8dffa8' : '#ffffff');
    root.add(u.tag);
    u.marker.visible = mate;
  }
  if (mate) {
    u.marker.position.y = 58 + Math.sin(now / 300) * 2;
    u.marker.rotation.y = now / 600 - u.facing;
  }

  // Face the aim direction, turning smoothly through the shortest angle.
  const want = Math.PI / 2 - p.aim;
  if (u.facing === null) u.facing = want;
  const diff = Math.atan2(Math.sin(want - u.facing), Math.cos(want - u.facing));
  u.facing += diff * Math.min(1, dt * 14);
  root.rotation.y = u.facing;

  const run = clamp(speed / 180, 0, 1);
  // Footsteps you can hear and place: one every 48 units walked. Crouching is silent.
  u.stepDist = (u.stepDist ?? 0) + speed * dt;
  if (u.stepDist > 48) {
    u.stepDist = 0;
    if (!p.crouch && !p.downed && !REPLAY) soundAt(root.position.x, root.position.z, p.swim ? SFX.splash : SFX.step, 420);
  }
  u.phase += dt * (2 + run * 11);
  const swing = Math.sin(u.phase) * 0.9 * run;
  const breathe = Math.sin(now / 600 + u.phase * 0.01);
  u.legL.rotation.x = swing;
  u.legR.rotation.x = -swing;
  u.armL.rotation.x = -swing * 0.8 + 0.05;
  u.armR.rotation.x = -Math.PI / 2 + Math.sin(u.phase * 2) * 0.05 * run; // gun arm stays raised
  u.torso.scale.y = 1 + breathe * 0.02;
  u.head.rotation.z = Math.sin(u.phase) * 0.05 * run;
  u.head.position.y = 29 + breathe * 0.15;

  // Downed players lie on their back; revived players stand up again.
  u.down = lerp(u.down, p.downed ? 1 : 0, Math.min(1, dt * 8));
  u.body.rotation.x = lerp(0.14 * run, -Math.PI / 2, u.down);
  u.body.position.y = lerp(Math.abs(Math.sin(u.phase)) * 1.4 * run, 4, u.down);
  // Crouching squashes the body to two thirds of its height.
  u.squat = lerp(u.squat ?? 0, p.crouch ? 1 : 0, Math.min(1, dt * 14));
  u.body.scale.y = 1 - 0.33 * u.squat;
  // Swimmers sit low in the water: only the head and shoulders show.
  u.sink = lerp(u.sink ?? 0, p.swim ? 1 : 0, Math.min(1, dt * 10));
  u.body.position.y -= 22 * u.sink;

  // Blink: the eyes squash shut for a moment every few seconds.
  if (now > u.blinkAt + 130) u.blinkAt = now + 2200 + Math.random() * 2600;
  const shut = now > u.blinkAt || p.downed;
  for (const eye of u.eyes) eye.scale.y = lerp(eye.scale.y, shut ? 0.12 : 1, 0.5);
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
// The models are authored small, so they sit in a root that scales them clear of the near plane.
const vmRoot = new THREE.Group();
vmRoot.scale.setScalar(6);
camera.add(vmRoot);
const flashLight = new THREE.PointLight(0xffc070, 0, 60, 2);
const matSleeve = new THREE.MeshStandardMaterial({ color: 0x3f7fe0, roughness: 0.6 });
const matHand = new THREE.MeshStandardMaterial({ color: 0xffd2ae, roughness: 0.55 });
const vmHandGeo = new RoundedBoxGeometry(0.075, 0.075, 0.09, 3, 0.03);
const vmArmGeo = new RoundedBoxGeometry(0.085, 0.085, 0.5, 3, 0.03);
const ACCENTS = { Pistol: 0xffb300, SMG: 0x26c6da, Rifle: 0xff7043, Shotgun: 0x9ccc65, Sniper: 0xab47bc };
function viewmodelFor(weaponName) {
  if (!viewmodels.has(weaponName)) {
    const g = new THREE.Group();
    const { muzzle } = weaponBuilders[weaponName](g);
    const flash = new THREE.Mesh(new THREE.SphereGeometry(0.05, 10, 8), matFlash);
    flash.position.copy(muzzle);
    flash.visible = false;
    g.add(flash);
    // A colored stripe tells the weapons apart at a glance.
    const accent = new THREE.MeshStandardMaterial({ color: ACCENTS[weaponName], roughness: 0.35, emissive: ACCENTS[weaponName], emissiveIntensity: 0.25 });
    g.add(part(box(0.064, 0.022, 0.1), accent, 0, 0.012, 0.02));
    // Hands and sleeves: the right hand on the grip, the left under the barrel of long guns.
    g.add(part(vmHandGeo, matHand, 0, -0.1, 0.08));
    g.add(part(vmArmGeo, matSleeve, 0.03, -0.2, 0.36, 0.38, -0.08, 0));
    if (weaponName !== 'Pistol') {
      g.add(part(vmHandGeo, matHand, 0, -0.075, -0.22));
      g.add(part(vmArmGeo, matSleeve, -0.13, -0.19, 0.0, 0.42, 0.62, 0));
    }
    g.traverse(o => { if (o.isMesh) o.castShadow = false; });
    g.userData.flash = flash;
    g.userData.muzzle = muzzle;
    viewmodels.set(weaponName, g);
    vmRoot.add(g);
  }
  return viewmodels.get(weaponName);
}
camera.add(flashLight);
flashLight.position.set(1.3, -1.2, -4);

// ---------- Overlay (minimap, kill feed, crosshair, HUD, touch controls) ----------
const overlay = document.getElementById('overlay');
const ctx = overlay.getContext('2d');
const hud = document.getElementById('status');
const startBtn = document.getElementById('start');
const backLink = document.getElementById('back');
const againBtn = document.getElementById('again');
const sideAlpha = document.getElementById('side-alpha');
const sideBravo = document.getElementById('side-bravo');
const STICK = { x: 110, y: 0, r: 70, hit: 90 };
const FIRE = { x: 0, y: 0, r: 55, hit: 65 };
const RELOAD = { x: 0, y: 0, r: 34, hit: 42 };
const SWAP = { x: 0, y: 0, r: 30, hit: 38 };
const REVIVE_BTN = { x: 0, y: 0, r: 34, hit: 42 };
const SHOP_BTN = { x: 0, y: 0, r: 30, hit: 38 };
const JUMP_BTN = { x: 0, y: 0, r: 34, hit: 42 };
const CROUCH_BTN = { x: 0, y: 0, r: 34, hit: 42 };
const NADE_BTN = { x: 0, y: 0, r: 30, hit: 38 };
const SCOPE_BTN = { x: 0, y: 0, r: 30, hit: 38 };
const CHAT_BTN = { x: 0, y: 0, r: 30, hit: 38 };
const NEAR_BTN = { x: 0, y: 0, r: 30, hit: 38 };
const MUTE_BTN = { x: 0, y: 0, r: 30, hit: 38 }, REPORT_BTN = { x: 0, y: 0, r: 30, hit: 38 };

let VW = innerWidth, VH = innerHeight;
function resize() {
  renderer.setSize(innerWidth, innerHeight);
  if (composer) composer.setSize(innerWidth, innerHeight);
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
  VW = innerWidth;
  VH = innerHeight;
  overlay.width = Math.round(VW * DPR);
  overlay.height = Math.round(VH * DPR);
  STICK.y = innerHeight - 110;
  FIRE.x = innerWidth - 110; FIRE.y = innerHeight - 110;
  RELOAD.x = innerWidth - 230; RELOAD.y = innerHeight - 110;
  SWAP.x = innerWidth - 110; SWAP.y = innerHeight - 220;
  REVIVE_BTN.x = innerWidth - 230; REVIVE_BTN.y = innerHeight - 220;
  SHOP_BTN.x = innerWidth - 110; SHOP_BTN.y = innerHeight - 310;
  JUMP_BTN.x = innerWidth - 340; JUMP_BTN.y = innerHeight - 165;
  CROUCH_BTN.x = innerWidth - 340; CROUCH_BTN.y = innerHeight - 75;
  NADE_BTN.x = innerWidth - 230; NADE_BTN.y = innerHeight - 310;
  SCOPE_BTN.x = innerWidth - 340; SCOPE_BTN.y = innerHeight - 255;
  CHAT_BTN.x = innerWidth - 460; CHAT_BTN.y = innerHeight - 110;
  NEAR_BTN.x = innerWidth - 460; NEAR_BTN.y = innerHeight - 165;
  MUTE_BTN.x = innerWidth - 580; MUTE_BTN.y = innerHeight - 110;
  REPORT_BTN.x = innerWidth - 580; REPORT_BTN.y = innerHeight - 165;
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
let pendingBuy = 0;
let pitch = 0;            // up or down aim, in radians; positive is up
let crouchToggle = false; // touch: the crouch button stays on until tapped again
let pendingJump = false;
let pendingThrow = false;
let scoped = false;       // looking through the sniper scope
let jumpZ = 0, jumpV = 0; // the local player's jump, shown at once and decided by the server
const isCrouching = () => keys.has('c') || keys.has('control') || crouchToggle;
function tryJump() {
  if (!canMove() || isCrouching() || jumpZ > 0) return;
  pendingJump = true;
  jumpV = JUMP_SPEED;
}
let reviveTouched = false;
const pointers = new Map();

function requestSwitch(slot) { pendingSwitch = slot; }
// nextOwned returns the next weapon slot the player owns, stepping forward or back.
function nextOwned(dir) {
  let slot = curr.ammo.slot;
  for (let i = 0; i < 5; i++) {
    slot = ((slot - 1 + dir + 5) % 5) + 1;
    if (curr.ammo.owned[slot - 1]) return slot;
  }
  return curr.ammo.slot;
}

addEventListener('keydown', e => {
  ensureAudio();
  const k = e.key.toLowerCase();
  if (e.repeat) return;
  keys.add(k);
  if (k === ' ') { e.preventDefault(); if (!REPLAY) tryJump(); }
  if (k === 'm') muted = !muted;
  if (k === 'r') pendingReload = true;
  if (k === 'g') pendingThrow = true;
  if (k === 'z') scoped = !scoped;
  if (k === 'b' && curr && curr.me.alive) { shopOpen = !shopOpen; bigMap = false; }
  if (k === 'tab' || k === 'n') { e.preventDefault(); bigMap = !bigMap; shopOpen = false; }
  if (k === 'escape') { shopOpen = false; bigMap = false; helpOpen = false; }
  if (k === 'h' || k === '?') { if (helpOpen) closeHelp(); else helpOpen = true; }
  if (k === 'k') toggleVoiceDeaf();
  if (k === 'o') reportLastSpeaker();
  if (k === 't') startTalk(true);
  if (k === 'y') startTalk(false);
  if (k === 'v' || k === 'x') toggleChatWheel(k === 'v' ? 'team' : 'near');
  if (chatWheel && k >= '1' && k <= String(CHAT_PRESETS.length)) { sendChat(Number(k) - 1); return; }
  if (chatWheel && k === 'escape') chatWheel = null;
  if (shopOpen && k >= '1' && k <= '7') pendingBuy = Number(k);
  else if (k >= '1' && k <= '5') requestSwitch(Number(k));
  if (k === 'q' && curr && curr.me.alive) {
    pendingPing = { x: curr.me.x + Math.cos(yaw) * 300, y: curr.me.y + Math.sin(yaw) * 300 };
  }
});
addEventListener('keyup', e => {
  const k = e.key.toLowerCase();
  keys.delete(k);
  if (k === 't' || k === 'y') stopTalk();
});
overlay.addEventListener('wheel', e => {
  if (!curr) return;
  requestSwitch(nextOwned(e.deltaY > 0 ? 1 : -1));
}, { passive: true });

overlay.style.touchAction = 'none';
const locked = () => document.pointerLockElement === overlay;
const overlayPos = e => {
  const r = overlay.getBoundingClientRect();
  return { x: e.clientX - r.left, y: e.clientY - r.top };
};
const dist = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);
const inRect = (p, r) => p.x >= r.x && p.x <= r.x + r.w && p.y >= r.y && p.y <= r.y + r.h;

overlay.addEventListener('pointerdown', e => {
  ensureAudio();
  overlay.setPointerCapture(e.pointerId);
  const p = overlayPos(e);
  if (e.pointerType === 'touch') touchActive = true;

  if (e.pointerType === 'touch') {
    if (helpOpen) { closeHelp(); pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (chatWheel) {
      const row = chatRows.find(r => inRect(p, r));
      if (row) sendChat(row.slot); else chatWheel = null;
      pointers.set(e.pointerId, { role: 'menu' });
      return;
    }
    // Open menus take the tap first: a shop row buys, the big map pings, anywhere else closes.
    if (shopOpen) {
      const row = shopRows.find(r => inRect(p, r));
      if (row) pendingBuy = row.slot; else shopOpen = false;
      pointers.set(e.pointerId, { role: 'menu' });
      return;
    }
    if (bigMap && curr) {
      const m = bigMapRect();
      if (inRect(p, { x: m.x, y: m.y, w: m.size, h: m.size })) {
        pendingPing = { x: (p.x - m.x) * 2000 / m.size, y: (p.y - m.y) * 2000 / m.size };
      } else bigMap = false;
      pointers.set(e.pointerId, { role: 'menu' });
      return;
    }
    if (dist(p, { x: RADAR.cx, y: RADAR.cy }) <= RADAR.r && curr) {
      bigMap = true;
      pointers.set(e.pointerId, { role: 'menu' });
      return;
    }
    if (dist(p, JUMP_BTN) <= JUMP_BTN.hit) { tryJump(); pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, NADE_BTN) <= NADE_BTN.hit) { pendingThrow = true; pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, CHAT_BTN) <= CHAT_BTN.hit) { toggleChatWheel('team'); pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, NEAR_BTN) <= NEAR_BTN.hit) { toggleChatWheel('near'); pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, MUTE_BTN) <= MUTE_BTN.hit) { toggleVoiceDeaf(); pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, REPORT_BTN) <= REPORT_BTN.hit) { reportLastSpeaker(); pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, SCOPE_BTN) <= SCOPE_BTN.hit && curr && curr.ammo.weapon === 'Sniper') { scoped = !scoped; pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, CROUCH_BTN) <= CROUCH_BTN.hit) { crouchToggle = !crouchToggle; pointers.set(e.pointerId, { role: 'menu' }); return; }
    if (dist(p, SHOP_BTN) <= SHOP_BTN.hit && curr && curr.me.alive) {
      shopOpen = true;
      pointers.set(e.pointerId, { role: 'menu' });
      return;
    }
    if (dist(p, STICK) <= STICK.hit) { pointers.set(e.pointerId, { role: 'stick' }); updateStick(p); return; }
    if (dist(p, FIRE) <= FIRE.hit) { fireHeld = true; pointers.set(e.pointerId, { role: 'fire' }); return; }
    if (dist(p, RELOAD) <= RELOAD.hit) { pendingReload = true; pointers.set(e.pointerId, { role: 'reload' }); return; }
    if (dist(p, REVIVE_BTN) <= REVIVE_BTN.hit) { reviveTouched = true; pointers.set(e.pointerId, { role: 'revive' }); return; }
    if (dist(p, SWAP) <= SWAP.hit) {
      if (curr) requestSwitch(nextOwned(1));
      pointers.set(e.pointerId, { role: 'swap' });
      return;
    }
    pointers.set(e.pointerId, { role: 'look', x: p.x, y: p.y });
    return;
  }
  // Mouse: the first click captures the pointer for mouse-look; while captured, the button fires.
  if (e.button === 2) return;
  if (!locked()) overlay.requestPointerLock();
  else shootHeld = true;
  pointers.set(e.pointerId, { role: 'mouse' });
});

// The right mouse button holds the sniper scope up.
overlay.addEventListener('mousedown', e => { if (e.button === 2) scoped = true; });
addEventListener('mouseup', e => { if (e.button === 2) scoped = false; });
overlay.addEventListener('contextmenu', e => e.preventDefault());

overlay.addEventListener('pointermove', e => {
  if (e.pointerType === 'mouse' && locked()) {
    yaw += e.movementX * 0.0025 * (camera.fov / 72); // slower while scoped
    pitch = clamp(pitch - e.movementY * 0.0022 * (camera.fov / 72), -MAX_PITCH, MAX_PITCH);
    return;
  }
  const rec = pointers.get(e.pointerId);
  if (!rec) return;
  const p = overlayPos(e);
  if (rec.role === 'look') {
    yaw += (p.x - rec.x) * 0.006;
    pitch = clamp(pitch - (p.y - rec.y) * 0.005, -MAX_PITCH, MAX_PITCH);
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
  if (rec.role === 'revive') reviveTouched = [...pointers.values()].some(r => r.role === 'revive');
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
let lastPhase = '';
let pingMs = 0; // smoothed round trip to the server, including its tick wait

function ensureAudio() {
  if (!audioCtx) audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  if (audioCtx.state === 'suspended') audioCtx.resume();
}
// Every sound goes through a stereo panner, so sounds in the world come from their direction.
// soundAt sets where the next sounds come from; plain calls play in the centre at full volume.
let sfxPan = 0, sfxGain = 1;
function sfxOut(node) {
  const pan = audioCtx.createStereoPanner();
  pan.pan.value = sfxPan;
  node.connect(pan).connect(audioCtx.destination);
}
// soundAt plays fn as if it happened at world position (x, y): quieter with distance, and panned
// left or right of where the camera faces. Nothing plays beyond maxDist.
function soundAt(x, y, fn, maxDist = 900) {
  if (!audioCtx || muted) return;
  const dx = x - camera.position.x, dy = y - camera.position.z, d = Math.hypot(dx, dy);
  if (d > maxDist) return;
  sfxGain = Math.pow(1 - d / maxDist, 1.6);
  sfxPan = d < 1 ? 0 : clamp((dx * -Math.sin(yaw) + dy * Math.cos(yaw)) / d, -1, 1) * 0.9;
  try { fn(); } finally { sfxPan = 0; sfxGain = 1; }
}

function tone(freq, dur, type = 'square', vol = 0.08, slideTo = null, delay = 0) {
  if (!audioCtx || muted) return;
  const t0 = audioCtx.currentTime + delay;
  const o = audioCtx.createOscillator();
  const g = audioCtx.createGain();
  o.type = type;
  o.frequency.setValueAtTime(freq, t0);
  if (slideTo) o.frequency.exponentialRampToValueAtTime(slideTo, t0 + dur);
  g.gain.setValueAtTime(vol * sfxGain, t0);
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  o.connect(g);
  sfxOut(g);
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
  g.gain.value = vol * sfxGain;
  src.buffer = buf;
  let node = src;
  if (cutoff) {
    const f = audioCtx.createBiquadFilter();
    f.type = 'lowpass';
    f.frequency.value = cutoff;
    node = src.connect(f);
  }
  node.connect(g);
  sfxOut(g);
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
  cash: () => { tone(1200, 0.06, 'sine', 0.07); tone(1600, 0.1, 'sine', 0.07, null, 0.06); },
  step: () => noiseBurst(0.07, 0.1, 500),
  splash: () => noiseBurst(0.22, 0.09, 1600),
  boom: (vol = 1) => { noiseBurst(0.5, 0.5 * vol, 700); tone(70, 0.5, 'sawtooth', 0.2 * vol, 25); },
  crate: () => { noiseBurst(0.16, 0.25, 1400); tone(180, 0.12, 'square', 0.06, 70); },
  buy: () => { tone(500, 0.06, 'square', 0.06); tone(750, 0.06, 'square', 0.06, null, 0.07); tone(1000, 0.1, 'square', 0.06, null, 0.14); },
};

// ---------- Networking ----------
const proto = location.protocol === 'https:' ? 'wss' : 'ws';
const ws = REPLAY ? null : new WebSocket(`${proto}://${location.host}/ws/${id}`);
let prev = null, curr = null, currAt = 0;

function onState(msg) {
  if (msg.t !== 'state') return;
  // The server leaves these lists out when they have not changed since the last snapshot.
  msg.obstacles ??= curr ? curr.obstacles : [];
  msg.loot ??= curr ? curr.loot : [];
  msg.grenades ??= [];
  if (msg.world) buildWorld(msg.world);
  if (msg.chat?.length) hearChat(msg.chat);
  updateCaptureMarkers(msg.capture);
  updateBombMarkers(msg.bomb);
  // Other players' shots: a tracer, and the weapon's sound from where it was fired.
  for (const sh of msg.shots ?? []) {
    if (window.__arena) window.__shotsHeard = (window.__shotsHeard || 0) + 1;
    const from = new THREE.Vector3(sh.x + Math.cos(sh.a) * 14, 22, sh.y + Math.sin(sh.a) * 14);
    const to = new THREE.Vector3(sh.x + Math.cos(sh.a) * sh.r, 22, sh.y + Math.sin(sh.a) * sh.r);
    const line = new THREE.Line(new THREE.BufferGeometry().setFromPoints([from, to]),
      new THREE.LineBasicMaterial({ color: 0xfff0b0, transparent: true, opacity: 0.85 }));
    scene.add(line);
    tracers.push({ obj: line, until: performance.now() + 110 });
    soundAt(sh.x, sh.y, SHOT_SOUND[sh.w] ?? SHOT_SOUND.Pistol);
  }
  if (msg.echo) pingMs = pingMs ? lerp(pingMs, performance.now() - msg.echo, 0.1) : performance.now() - msg.echo;

  if (curr) {
    if (msg.me.alive && msg.me.hp > curr.me.hp + 1) SFX.pickup();
    if (msg.pings.length > curr.pings.length) SFX.ping();
    if (msg.ammo.slot !== curr.ammo.slot) SFX.switch();
    if (msg.ammo.credits > curr.ammo.credits) SFX.cash();
    if (msg.ammo.credits < curr.ammo.credits) SFX.buy();
  }
  // Rounds mode: the armory opens by itself in each buy phase and closes when the fight starts.
  const phase = msg.round ? msg.round.phase : '';
  if (phase !== lastPhase) {
    if (phase === 'buy') { shopOpen = msg.me.alive; bigMap = false; SFX.ping(); }
    if (phase === 'fight') { shopOpen = false; SFX.zone(); }
    lastPhase = phase;
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
  reconcile(msg);

  startBtn.hidden = msg.started;
  // Once you are out, or the match is over, the next match is one tap away.
  const out = msg.over || (msg.started && !msg.me.alive && !msg.round);
  backLink.hidden = !out;
  againBtn.hidden = !out;
  // Rounds rooms let you pick a side while waiting; "Play again" returns to the same kind of match.
  const picking = !msg.started && msg.lobby && msg.lobby.mode === 'rounds';
  sideAlpha.hidden = sideBravo.hidden = !picking;
  if (msg.round || picking) againBtn.setAttribute('hx-vals', '{"mode":"rounds"}');
  if (picking) {
    const l = msg.lobby;
    hud.textContent = `Rounds · ${msg.alive} here · you: ${['no side', 'Alpha', 'Bravo'][l.side]} · Alpha ${l.alpha} · Bravo ${l.bravo}`;
  } else if (!msg.started) hud.textContent = `${msg.alive} in the room · press Start, bots fill the empty spots`;
  else if (msg.over) hud.textContent = msg.me.place === 1 ? 'Victory!' : `Winner: ${msg.winner}`;
  else if (msg.me.downed) hud.textContent = `DOWNED · bleeding out ${Math.ceil(msg.me.bleed)}s · teammate: hold E`;
  else if (msg.round) {
    const r = msg.round, secs = Math.ceil(r.left);
    const clock = `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}`;
    hud.textContent = `Round ${r.num} · ${r.team} ${r.mine} : ${r.theirs} · ${r.phase === 'buy' ? 'Buy' : r.phase === 'end' ? 'Next round' : 'Fight'} ${clock}`
      + (msg.bomb ? ` · ${bombText(msg.bomb)}` : '');
  }
  else if (msg.capture) hud.textContent = captureText(msg.capture);
  else if (msg.me.alive) hud.textContent = `Alive: ${msg.alive} · HP ${Math.round(msg.me.hp)}`;
  else hud.textContent = `Eliminated · placed #${msg.me.place}`;
  renderObjectives(msg.capture);
}
// ?lag=150 delays everything sent and received by that many milliseconds, to try the game as it
// would feel on a slow connection.
const LAG = Math.max(0, Number(new URLSearchParams(location.search).get('lag')) || 0);
function sendToServer(text) {
  if (LAG) setTimeout(() => { if (ws.readyState === WebSocket.OPEN) ws.send(text); }, LAG);
  else ws.send(text);
}
if (ws) ws.binaryType = 'arraybuffer';
if (ws) ws.onmessage = e => {
  if (typeof e.data !== 'string') { onVoice(e.data); return; }
  const msg = JSON.parse(e.data);
  if (LAG) setTimeout(() => onState(msg), LAG); else onState(msg);
};

// ---------- Movement prediction ----------
// The server decides where everyone is, but waiting for it makes your own movement feel late.
// So the client moves you at once using the server's rules, remembers the inputs the server has
// not answered yet, and on every snapshot starts again from the server's position and replays them.
const MOVE_SPEED = 180, PLAYER_R = 12, WORLD = 2000, STEP = 1 / 30; // must match match.go
const pendingMoves = [];           // inputs sent but not yet reflected in a snapshot: { ts, dx, dy }
const pred = { x: 0, y: 0 };       // where the server will put us once it has caught up
const shown = { x: 0, y: 0 };      // what the camera uses: pred, smoothed
let predicting = false;
let lastMove = { dx: 0, dy: 0, at: 0 };

// canMove mirrors the cases where the server ignores movement.
function canMove() {
  return !!curr && curr.started && !curr.over && curr.me.alive && !curr.me.downed
    && !(curr.round && curr.round.phase !== 'fight');
}
function blockedAt(x, y) {
  for (const o of curr.obstacles) {
    if (x > o.x - PLAYER_R && x < o.x + o.w + PLAYER_R && y > o.y - PLAYER_R && y < o.y + o.h + PLAYER_R) return true;
  }
  return false;
}
// moveFor moves a position for a stretch of time under the server's rules: each axis on its own,
// so you slide along walls. Long stretches are cut into tick-sized steps so walls are not skipped.
function moveFor(pos, dx, dy, seconds) {
  for (let left = Math.min(seconds, 0.5); left > 1e-6; left -= STEP) {
    const dt = Math.min(STEP, left);
    const nx = clamp(pos.x + dx * MOVE_SPEED * dt, 0, WORLD);
    const ny = clamp(pos.y + dy * MOVE_SPEED * dt, 0, WORLD);
    if (!blockedAt(nx, pos.y)) pos.x = nx;
    if (!blockedAt(pos.x, ny)) pos.y = ny;
  }
}
// reconcile runs on every snapshot. The server keeps applying an input until the next one
// arrives, so each remembered input counts for the time until the one after it; the newest one
// is still running and is carried forward frame by frame in shownPosition.
function reconcile(msg) {
  // Keep the input the server has just answered: it is still in force until the next one lands.
  while (pendingMoves.length > 1 && pendingMoves[1].ts <= msg.echo) pendingMoves.shift();
  pred.x = msg.me.x;
  pred.y = msg.me.y;
  if (!canMove()) { pendingMoves.length = 0; predicting = false; return; }
  for (let i = 0; i + 1 < pendingMoves.length; i++) {
    // The answered input has already moved us on the server up to the snapshot; later ones in full.
    const from = Math.max(pendingMoves[i].ts, msg.echo ? msg.echo : pendingMoves[i].ts);
    moveFor(pred, pendingMoves[i].dx, pendingMoves[i].dy, (pendingMoves[i + 1].ts - from) / 1000);
  }
  if (!predicting || Math.hypot(pred.x - shown.x, pred.y - shown.y) > 200) { shown.x = pred.x; shown.y = pred.y; }
  predicting = true;
}
// shownPosition is where to draw the local player this frame.
function shownPosition(dt, now) {
  const target = { x: pred.x, y: pred.y };
  moveFor(target, lastMove.dx, lastMove.dy, (now - lastMove.at) / 1000);
  const k = Math.min(1, dt * 22);
  shown.x += (target.x - shown.x) * k;
  shown.y += (target.y - shown.y) * k;
  return shown;
}

// Movement is sent in world coordinates, relative to where the player is looking.
setInterval(() => {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  const fx = Math.cos(yaw), fy = Math.sin(yaw);
  const rx = -Math.sin(yaw), ry = Math.cos(yaw);
  const forward = (keys.has('w') || keys.has('arrowup') ? 1 : 0) - (keys.has('s') || keys.has('arrowdown') ? 1 : 0) - stick.dy;
  const strafe = (keys.has('d') || keys.has('arrowright') ? 1 : 0) - (keys.has('a') || keys.has('arrowleft') ? 1 : 0) + stick.dx;
  let dx = fx * forward + rx * strafe;
  let dy = fy * forward + ry * strafe;
  const len = Math.hypot(dx, dy);
  if (len > 1) { dx /= len; dy /= len; }

  const shoot = (shootHeld || fireHeld || keys.has('f')) && !shopOpen && !bigMap;
  if (shoot && curr && curr.me.alive && !curr.me.downed && curr.ammo.mag > 0 && !curr.ammo.reloading && performance.now() - lastShotAt > 60) {
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
  const revive = keys.has('e') || reviveTouched;
  const buy = pendingBuy;
  pendingBuy = 0;
  const ts = performance.now();
  const crouch = isCrouching();
  const throwNow = pendingThrow;
  pendingThrow = false;
  const jump = pendingJump;
  pendingJump = false;
  if (canMove()) {
    moveFor(pred, lastMove.dx, lastMove.dy, (ts - lastMove.at) / 1000); // the input that just ended
    // Predicted movement uses the speed the server will: slower while crouching.
    const k = inWaterAt(pred.x, pred.y) ? SWIM_SPEED : crouch ? CROUCH_SPEED : 1;
    pendingMoves.push({ ts, dx: dx * k, dy: dy * k });
    if (pendingMoves.length > 120) pendingMoves.shift();
    lastMove = { dx: dx * k, dy: dy * k, at: ts };
  } else {
    lastMove = { dx: 0, dy: 0, at: ts };
  }
  sendToServer(JSON.stringify({ t: 'input', dx, dy, aim: yaw, shoot, ping, px, py, reload, switch: sw, revive, buy, ts, pitch, crouch, jump, throw: throwNow }));
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
  const flat = Math.cos(pitch) * range;
  const end = new THREE.Vector3(curr.me.x + Math.cos(yaw) * flat, Math.max(0, camera.position.y + Math.sin(pitch) * range),
    curr.me.y + Math.sin(yaw) * flat);
  const geo = new THREE.BufferGeometry().setFromPoints([muzzle, end]);
  const line = new THREE.Line(geo, new THREE.LineBasicMaterial({ color: 0xfff0b0, transparent: true, opacity: 0.8 }));
  scene.add(line);
  tracers.push({ obj: line, until: performance.now() + 90 });
}

// ---------- Scene sync ----------
function syncPlayers(t, dt, now) {
  const prevById = new Map(prev.players.map(p => [p.id, p]));
  const seen = new Set();
  for (const p of curr.players) {
    seen.add(p.id);
    let mesh = playerMeshes.get(p.id);
    if (!mesh) { mesh = makePlayerMesh(p); playerMeshes.set(p.id, mesh); }
    const old = prevById.get(p.id) ?? p;
    mesh.position.set(lerp(old.x, p.x, t), lerp(old.z ?? 0, p.z ?? 0, t), lerp(old.y, p.y, t));
    const speed = Math.hypot(p.x - old.x, p.y - old.y) * (REPLAY ? 1000 / R.frameMs : 30);
    // A spectator has no side, so in a replay everyone keeps their own colours.
    const mate = !REPLAY && curr.me.team !== 0 && p.team === curr.me.team;
    animateAvatar(mesh, p, speed, dt, now, mate);
  }
  for (const [pid, mesh] of playerMeshes) {
    if (!seen.has(pid)) { disposeAvatar(mesh); playerMeshes.delete(pid); }
  }
}

function syncObstacles() {
  const seen = new Set();
  for (const r of curr.obstacles) {
    const key = `${r.x},${r.y},${r.w},${r.h}`;
    seen.add(key);
    if (!obstacleMeshes.has(key)) {
      const m = r.k === 'tree' ? makeTree(r) : r.k === 'crate' ? makeCrate(r) : r.k === 'container' ? makeContainer(r) : makeBuilding(r);
      scene.add(m);
      obstacleMeshes.set(key, m);
    }
  }
  for (const [key, m] of obstacleMeshes) {
    if (!seen.has(key)) {
      // A crate that vanishes close by was just broken: play the crack.
      if (m.userData.crate && m.position.distanceTo(camera.position) < 500) SFX.crate();
      scene.remove(m);
      obstacleMeshes.delete(key);
    }
  }
}

function syncLoot(now) {
  const seen = new Set();
  for (const l of curr.loot) {
    seen.add(l.id);
    let m = lootMeshes.get(l.id);
    if (!m) {
      m = l.k === 'cash' ? new THREE.Mesh(coinGeo, matCoin) : new THREE.Mesh(lootGeo, matLoot);
      m.castShadow = true;
      scene.add(m);
      lootMeshes.set(l.id, m);
    }
    m.position.set(l.x, 9 + Math.sin(now / 300 + l.id) * 2, l.y);
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

// Grenades in the air, and the blast when one goes off.
const grenadeMeshes = new Map();
const grenadeGeo = new THREE.SphereGeometry(2.6, 12, 10);
const matGrenade = new THREE.MeshStandardMaterial({ color: 0x3d5a2a, roughness: 0.5, metalness: 0.4 });
const blastGeo = new THREE.SphereGeometry(1, 20, 14);
const blasts = [];
function syncGrenades(now) {
  const seen = new Set();
  for (const g of curr.grenades) {
    seen.add(g.id);
    let m = grenadeMeshes.get(g.id);
    if (!m) { m = new THREE.Mesh(grenadeGeo, matGrenade); m.castShadow = true; scene.add(m); grenadeMeshes.set(g.id, m); }
    m.position.set(g.x, g.z, g.y);
    m.rotation.x = now / 90;
  }
  for (const [gid, m] of grenadeMeshes) {
    if (seen.has(gid)) continue;
    const ball = new THREE.Mesh(blastGeo, new THREE.MeshBasicMaterial({ color: 0xffb347, transparent: true, opacity: 0.85, depthWrite: false, fog: false }));
    ball.position.copy(m.position);
    scene.add(ball);
    blasts.push({ ball, start: now });
    soundAt(m.position.x, m.position.z, () => SFX.boom(1), 1400);
    scene.remove(m);
    grenadeMeshes.delete(gid);
  }
  for (let i = blasts.length - 1; i >= 0; i--) {
    const f = (now - blasts[i].start) / 380;
    if (f >= 1) { scene.remove(blasts[i].ball); blasts[i].ball.material.dispose(); blasts.splice(i, 1); continue; }
    blasts[i].ball.scale.setScalar(8 + (BLAST_RADIUS - 8) * Math.sqrt(f));
    blasts[i].ball.material.opacity = 0.85 * (1 - f);
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
  zoneMesh.position.set(curr.zone.x, 150, curr.zone.y);
  zoneMesh.scale.set(r, 1, r);
  // The shadow light follows the player so nearby shadows stay sharp.
  sun.position.set(meX + SUN_DIR.x * 900, SUN_DIR.y * 900, meY + SUN_DIR.z * 900);
  sun.target.position.set(meX, 0, meY);
  sun.target.updateMatrixWorld();
}

// ---------- Viewmodel (weapon in view) ----------
let vmName = '';
let vmRaise = 0;     // 1 right after a weapon switch, easing to 0 as the new weapon comes up
let walkPhase = 0;   // advances while the local player moves
let walkAmount = 0;  // 0 standing, 1 at full speed, smoothed
function updateViewmodel(now) {
  if (REPLAY) { for (const vm of viewmodels.values()) vm.visible = false; return; }
  const name = curr.ammo.weapon;
  for (const [wName, vm] of viewmodels) vm.visible = wName === name && curr.me.alive && !curr.me.swim && camera.fov > 50;
  const vm = viewmodelFor(name);

  if (name !== vmName) { vmName = name; vmRaise = 1; }
  vmRaise = Math.max(0, vmRaise - 0.07);
  recoil = Math.max(0, recoil - 0.12);
  const reloadFrac = curr.ammo.reloading ? curr.ammo.reloadFrac : 0;
  const lower = (curr.ammo.reloading ? Math.sin(reloadFrac * Math.PI) * 0.28 : 0) + vmRaise * vmRaise * 0.3;
  const sway = Math.sin(now / 420) * 0.004;
  // The weapon bobs with each step and settles when standing still.
  const bobX = Math.sin(walkPhase) * 0.012 * walkAmount;
  const bobY = Math.abs(Math.cos(walkPhase)) * 0.01 * walkAmount;
  vm.position.set(0.25 + sway + bobX, -0.23 - lower - bobY, -0.74 + recoil * 0.06);
  vm.rotation.set(-recoil * 0.07 + lower * 0.5, 0, bobX * 0.6);

  const flashing = now < flashUntil;
  vm.userData.flash.visible = flashing;
  flashLight.intensity = flashing ? 2.5 : 0;
}

// ---------- Camera ----------
function updateCamera(meX, meY, dt) {
  if (REPLAY) { spectatorCamera(meX, meY, dt); return; }
  // While predicting, the step bob follows your input, not the server's delayed echo of it.
  const speed = predicting ? Math.hypot(lastMove.dx, lastMove.dy) * MOVE_SPEED
    : Math.hypot(curr.me.x - prev.me.x, curr.me.y - prev.me.y) * 30;
  walkAmount = lerp(walkAmount, clamp(speed / 180, 0, 1), Math.min(1, dt * 10));
  const before = Math.floor(walkPhase / Math.PI);
  walkPhase += dt * 11 * walkAmount;
  // Your own footsteps, one per step of the bob; splashes in water; silence while crouched.
  if (Math.floor(walkPhase / Math.PI) !== before && walkAmount > 0.3 && jumpZ === 0) {
    if (curr.me.swim) SFX.splash(); else if (!isCrouching()) SFX.step();
  }
  // A downed player's view drops to the ground.
  eyeHeight = lerp(eyeHeight, curr.me.swim ? SWIM_EYE : curr.me.downed ? 8 : isCrouching() ? CROUCH_EYE : EYE, Math.min(1, dt * 12));
  // The sniper scope narrows the view.
  if (curr.ammo.weapon !== 'Sniper' || !curr.me.alive || curr.me.swim) scoped = false;
  const fov = scoped ? 18 : 72;
  if (Math.abs(camera.fov - fov) > 0.1) { camera.fov = lerp(camera.fov, fov, Math.min(1, dt * 14)); camera.updateProjectionMatrix(); }
  for (const w of waterMeshes) w.position.y = 2.2 + Math.sin(performance.now() / 700 + w.position.x) * 0.25;
  // Your own jump is shown at once, with the same arc the server uses.
  if (jumpZ > 0 || jumpV > 0) {
    jumpZ += jumpV * dt;
    jumpV -= GRAVITY * dt;
    if (jumpZ <= 0 || !canMove()) { jumpZ = 0; jumpV = 0; }
  }
  const y = eyeHeight + jumpZ + Math.abs(Math.sin(walkPhase)) * 0.7 * walkAmount;
  camera.position.set(meX, y, meY);
  const flat = Math.cos(pitch) * 100;
  camera.lookAt(meX + Math.cos(yaw) * flat, y + Math.sin(pitch) * 100, meY + Math.sin(yaw) * flat);
  skyDome.position.copy(camera.position);
  for (const c of clouds.children) {
    c.position.x += dt * 6;
    if (c.position.x > 3600) c.position.x = -1600;
  }
}
let eyeHeight = EYE;

// ---------- Overlay drawing: a sci-fi heads-up display ----------
const C = {
  main: '#6ff3ff', dim: 'rgba(111,243,255,0.38)', faint: 'rgba(111,243,255,0.14)', bg: 'rgba(6,16,30,0.66)',
  warn: '#ffc94a', bad: '#ff5470', good: '#7dffa6', text: '#eaffff', gold: '#ffd54f',
};
const font = (px, weight = 700) => `${weight} ${px}px ui-monospace, "SF Mono", Menlo, Consolas, monospace`;
const glow = (color, blur = 8) => { ctx.shadowColor = color; ctx.shadowBlur = blur; };
const noGlow = () => { ctx.shadowBlur = 0; };

// panel draws a dark plate with two cut corners and a thin glowing edge.
function panel(x, y, w, h, cut = 12, edge = C.dim, fill = C.bg) {
  ctx.beginPath();
  ctx.moveTo(x + cut, y); ctx.lineTo(x + w, y); ctx.lineTo(x + w, y + h - cut);
  ctx.lineTo(x + w - cut, y + h); ctx.lineTo(x, y + h); ctx.lineTo(x, y + cut);
  ctx.closePath();
  ctx.fillStyle = fill;
  ctx.fill();
  ctx.strokeStyle = edge;
  ctx.lineWidth = 1.5;
  ctx.stroke();
  // Corner accent.
  ctx.strokeStyle = C.main;
  ctx.lineWidth = 2.5;
  ctx.beginPath(); ctx.moveTo(x, y + cut + 10); ctx.lineTo(x, y + cut); ctx.lineTo(x + cut, y); ctx.lineTo(x + cut + 10, y); ctx.stroke();
}

function drawCrosshair(bloom) {
  const gap = 5 + bloom * 200;
  const cx = VW / 2, cy = VH / 2;
  glow(C.main, 6);
  ctx.strokeStyle = C.main;
  ctx.lineWidth = 2;
  ctx.beginPath();
  ctx.moveTo(cx - gap - 9, cy); ctx.lineTo(cx - gap, cy);
  ctx.moveTo(cx + gap, cy);     ctx.lineTo(cx + gap + 9, cy);
  ctx.moveTo(cx, cy - gap - 9); ctx.lineTo(cx, cy - gap);
  ctx.moveTo(cx, cy + gap);     ctx.lineTo(cx, cy + gap + 9);
  ctx.stroke();
  ctx.fillStyle = C.text;
  ctx.fillRect(cx - 1, cy - 1, 2, 2);
  // Thin bracket ring that widens with spread.
  ctx.strokeStyle = C.dim;
  ctx.lineWidth = 1;
  const r = gap + 16;
  for (let q = 0; q < 4; q++) {
    ctx.beginPath();
    ctx.arc(cx, cy, r, q * Math.PI / 2 + 0.55, q * Math.PI / 2 + 1.02);
    ctx.stroke();
  }
  noGlow();
}

function drawHitMarker(now) {
  if (now > hitFlash.until) return;
  const cx = VW / 2, cy = VH / 2;
  const head = hitFlash.kind === 'head';
  glow(head ? C.bad : C.text, 8);
  ctx.strokeStyle = head ? C.bad : C.text;
  ctx.lineWidth = 3;
  ctx.beginPath();
  for (const [dx, dy] of [[-1, -1], [1, -1], [-1, 1], [1, 1]]) {
    ctx.moveTo(cx + dx * 8, cy + dy * 8);
    ctx.lineTo(cx + dx * 16, cy + dy * 16);
  }
  ctx.stroke();
  if (head) {
    ctx.fillStyle = C.bad;
    ctx.font = font(15);
    ctx.textAlign = 'center';
    ctx.fillText('HEADSHOT', cx, cy - 30);
  }
  noGlow();
}

// Compass strip under the status line: east is +x and south is +y in world space.
function drawCompass() {
  const w = Math.min(380, VW - 380), x0 = VW / 2 - w / 2, y = 62;
  if (w < 160) return;
  const heading = ((yaw * 180 / Math.PI + 90) % 360 + 360) % 360;
  ctx.save();
  ctx.beginPath(); ctx.rect(x0, y - 4, w, 30); ctx.clip();
  ctx.textAlign = 'center';
  const pxPerDeg = w / 120;
  for (let d = Math.floor((heading - 60) / 15) * 15; d <= heading + 60; d += 15) {
    const x = VW / 2 + (d - heading) * pxPerDeg;
    const deg = ((d % 360) + 360) % 360;
    const major = deg % 90 === 0;
    ctx.fillStyle = major ? C.main : C.dim;
    ctx.fillRect(x - 1, y, 2, major ? 9 : 5);
    if (deg % 45 === 0) {
      ctx.font = font(major ? 12 : 10);
      ctx.fillText({ 0: 'N', 45: 'NE', 90: 'E', 135: 'SE', 180: 'S', 225: 'SW', 270: 'W', 315: 'NW' }[deg], x, y + 22);
    }
  }
  ctx.restore();
  glow(C.main, 6);
  ctx.fillStyle = C.main;
  ctx.beginPath(); ctx.moveTo(VW / 2 - 5, y - 6); ctx.lineTo(VW / 2 + 5, y - 6); ctx.lineTo(VW / 2, y); ctx.fill();
  noGlow();
}

function drawAmmo() {
  const a = curr.ammo;
  // Bottom right on desktop; on touch the fire buttons live there, so the panel moves up.
  const w = 250, h = 96, x = VW - w - 20, y = touchActive ? 150 : VH - h - 20;
  panel(x, y, w, h);
  ctx.textAlign = 'left';
  ctx.fillStyle = C.main;
  ctx.font = font(12);
  ctx.fillText(`${a.weapon.toUpperCase()}${a.auto ? ' // AUTO' : ' // SEMI'}`, x + 16, y + 22);
  ctx.textAlign = 'right';
  ctx.fillStyle = C.gold;
  ctx.fillText(`CR ${a.credits}`, x + w - 14, y + 22);
  ctx.textAlign = 'left';
  glow(a.mag === 0 ? C.bad : C.main, 10);
  ctx.fillStyle = a.mag === 0 ? C.bad : C.text;
  ctx.font = font(36, 800);
  ctx.fillText(String(a.mag).padStart(2, '0'), x + 16, y + 62);
  noGlow();
  const magW = ctx.measureText(String(a.mag).padStart(2, '0')).width;
  ctx.fillStyle = C.dim;
  ctx.font = font(16);
  ctx.fillText(`/ ${a.reserve}`, x + 24 + magW, y + 62);
  // Weapon slots: owned ones lit, the one in hand filled.
  for (let i = 0; i < 5; i++) {
    const sx = x + 16 + i * 30, sy = y + 72;
    const owned = a.owned[i], inHand = a.slot === i + 1;
    ctx.fillStyle = inHand ? C.main : owned ? C.faint : 'rgba(255,255,255,0.04)';
    ctx.fillRect(sx, sy, 24, 14);
    ctx.strokeStyle = owned ? C.dim : 'rgba(255,255,255,0.12)';
    ctx.lineWidth = 1;
    ctx.strokeRect(sx + 0.5, sy + 0.5, 23, 13);
    ctx.fillStyle = inHand ? '#06202a' : owned ? C.text : 'rgba(255,255,255,0.3)';
    ctx.font = font(10);
    ctx.textAlign = 'center';
    ctx.fillText(String(i + 1), sx + 12, sy + 11);
  }
  ctx.textAlign = 'left';
  if (a.reloading) {
    ctx.fillStyle = C.faint;
    ctx.fillRect(x + 176, y + 76, 60, 6);
    ctx.fillStyle = C.warn;
    ctx.fillRect(x + 176, y + 76, Math.max(2, 60 * a.reloadFrac), 6);
    ctx.font = font(9);
    ctx.fillText('RELOAD', x + 176, y + 71);
  } else if (!touchActive) {
    ctx.fillStyle = C.dim;
    ctx.font = font(9);
    ctx.fillText('[B] SHOP', x + 184, y + 84);
    ctx.fillStyle = a.grenades ? C.warn : C.dim;
    ctx.fillText(`[G] GRENADE x${a.grenades}`, x + 140, y + 60);
  }
}

// Health: twenty segments, bottom left (bottom centre on touch).
function drawHealth() {
  const hp = clamp(curr.me.hp, 0, 100);
  const w = 270, h = 46, x = touchActive ? VW / 2 - w / 2 : 20, y = VH - h - 20;
  const downed = curr.me.downed;
  const color = downed ? C.bad : hp > 60 ? C.good : hp > 30 ? C.warn : C.bad;
  panel(x, y, w, h, 10, downed ? C.bad : C.dim);
  ctx.textAlign = 'left';
  ctx.fillStyle = color;
  ctx.font = font(11);
  ctx.fillText(downed ? 'DOWNED // BLEEDING OUT' : REPLAY ? `SPECTATING ${curr.me.name.toUpperCase()}`
    : curr.me.swim ? 'SWIMMING // NO WEAPONS' : curr.me.hidden ? 'HIDDEN IN BUSH' : curr.ammo.armor > 0 ? `VITALS // ARMOUR ${curr.ammo.armor}` : 'VITALS', x + 14, y + 17);
  ctx.textAlign = 'right';
  ctx.fillStyle = C.text;
  ctx.font = font(15, 800);
  ctx.fillText(downed ? `${Math.ceil(curr.me.bleed)}s` : String(Math.round(hp)), x + w - 12, y + 19);
  const frac = downed ? clamp(curr.me.bleed / 20, 0, 1) : hp / 100;
  const segs = 20, sw = (w - 28) / segs;
  glow(color, 6);
  for (let i = 0; i < segs; i++) {
    ctx.fillStyle = i < Math.ceil(frac * segs) ? color : 'rgba(255,255,255,0.08)';
    if (i >= Math.ceil(frac * segs)) noGlow();
    ctx.fillRect(x + 14 + i * sw, y + 26, sw - 2.5, 10);
  }
  noGlow();
}

// Squad list under the radar: each teammate's health, state, and distance.
function drawSquad() {
  if (REPLAY || curr.me.team === 0) return;
  const mates = curr.players.filter(p => p.team === curr.me.team);
  mates.forEach((mate, i) => {
    const x = 20, y = RADAR.cy + RADAR.r + 40 + i * 34, w = 156;
    const color = mate.downed ? C.warn : mate.hp > 60 ? C.good : mate.hp > 30 ? C.warn : C.bad;
    panel(x, y, w, 28, 8);
    ctx.textAlign = 'left';
    ctx.fillStyle = C.text;
    ctx.font = font(10, 800);
    ctx.fillText(mate.name.slice(0, 11).toUpperCase(), x + 10, y + 12);
    ctx.textAlign = 'right';
    ctx.fillStyle = color;
    const dist = Math.round(Math.hypot(mate.x - curr.me.x, mate.y - curr.me.y) / 10);
    ctx.fillText(mate.downed ? `DOWN ${Math.ceil(mate.bleed)}s` : `${dist}m`, x + w - 8, y + 12);
    ctx.fillStyle = 'rgba(255,255,255,0.1)';
    ctx.fillRect(x + 10, y + 17, w - 20, 5);
    ctx.fillStyle = color;
    ctx.fillRect(x + 10, y + 17, (w - 20) * (mate.downed ? clamp(mate.bleed / 20, 0, 1) : clamp(mate.hp, 0, 100) / 100), 5);
  });
}

// ---------- Radar and tactical map ----------
const RADAR = { cx: 98, cy: 98, r: 78, range: 620 };
const knownObstacles = new Map(); // every building and tree seen so far, for the full map
let bigMap = false;

function rememberObstacles() {
  // Crates can be destroyed, so the full map leaves them out.
  for (const r of curr.obstacles) if (r.k !== 'crate') knownObstacles.set(`${r.x},${r.y}`, r);
}

// The radar turns with the player: whatever is ahead is at the top.
function drawRadar(now) {
  const { cx, cy, r, range } = RADAR;
  const k = r / range;
  const fx = Math.cos(yaw), fy = Math.sin(yaw);
  const toScreen = (wx, wy) => {
    const dx = wx - curr.me.x, dy = wy - curr.me.y;
    return [cx + (-dx * fy + dy * fx) * k, cy - (dx * fx + dy * fy) * k];
  };
  ctx.save();
  ctx.beginPath(); ctx.arc(cx, cy, r, 0, Math.PI * 2); ctx.clip();
  const bg = ctx.createRadialGradient(cx, cy, 0, cx, cy, r);
  bg.addColorStop(0, 'rgba(10,36,52,0.82)');
  bg.addColorStop(1, 'rgba(4,12,24,0.86)');
  ctx.fillStyle = bg;
  ctx.fillRect(cx - r, cy - r, r * 2, r * 2);
  // Range rings and cross lines.
  ctx.strokeStyle = C.faint;
  ctx.lineWidth = 1;
  for (const rr of [r / 3, (2 * r) / 3]) { ctx.beginPath(); ctx.arc(cx, cy, rr, 0, Math.PI * 2); ctx.stroke(); }
  ctx.beginPath(); ctx.moveTo(cx - r, cy); ctx.lineTo(cx + r, cy); ctx.moveTo(cx, cy - r); ctx.lineTo(cx, cy + r); ctx.stroke();
  // Sweep line.
  const sweep = (now / 1400) % (Math.PI * 2);
  const grad = ctx.createConicGradient(sweep, cx, cy);
  grad.addColorStop(0, 'rgba(111,243,255,0.22)');
  grad.addColorStop(0.12, 'rgba(111,243,255,0)');
  grad.addColorStop(1, 'rgba(111,243,255,0)');
  ctx.fillStyle = grad;
  ctx.fillRect(cx - r, cy - r, r * 2, r * 2);

  // Lakes and bushes.
  for (const l of world.lakes) {
    const [lx, ly] = toScreen(l.x, l.y);
    ctx.fillStyle = 'rgba(47,155,230,0.55)';
    ctx.beginPath(); ctx.arc(lx, ly, l.r * k, 0, Math.PI * 2); ctx.fill();
  }
  for (const b of world.bushes) {
    const [bx, by] = toScreen(b.x, b.y);
    ctx.fillStyle = 'rgba(125,255,166,0.28)';
    ctx.beginPath(); ctx.arc(bx, by, b.r * k, 0, Math.PI * 2); ctx.fill();
  }

  // Safe zone edge.
  const [zx, zy] = toScreen(curr.zone.x, curr.zone.y);
  ctx.strokeStyle = C.bad;
  ctx.lineWidth = 2;
  ctx.beginPath(); ctx.arc(zx, zy, curr.zone.r * k, 0, Math.PI * 2); ctx.stroke();

  // Buildings as true shapes, trees as dots.
  for (const o of curr.obstacles) {
    if (o.k === 'tree' || o.k === 'crate') {
      const [tx, ty] = toScreen(o.x + o.w / 2, o.y + o.h / 2);
      ctx.fillStyle = o.k === 'tree' ? 'rgba(125,255,166,0.55)' : 'rgba(255,170,80,0.9)';
      if (o.k === 'tree') { ctx.beginPath(); ctx.arc(tx, ty, 2, 0, Math.PI * 2); ctx.fill(); }
      else { const cs = Math.max(3, o.w * k); ctx.fillRect(tx - cs / 2, ty - cs / 2, cs, cs); }
      continue;
    }
    ctx.beginPath();
    [[o.x, o.y], [o.x + o.w, o.y], [o.x + o.w, o.y + o.h], [o.x, o.y + o.h]].forEach(([wx, wy], i) => {
      const [sx, sy] = toScreen(wx, wy);
      if (i) ctx.lineTo(sx, sy); else ctx.moveTo(sx, sy);
    });
    ctx.closePath();
    ctx.fillStyle = 'rgba(111,243,255,0.2)';
    ctx.fill();
    ctx.strokeStyle = C.dim;
    ctx.lineWidth = 1;
    ctx.stroke();
  }
  for (const l of curr.loot) {
    const [sx, sy] = toScreen(l.x, l.y);
    ctx.fillStyle = l.k === 'cash' ? C.gold : '#ff8fa3';
    ctx.fillRect(sx - 1.5, sy - 1.5, 3, 3);
  }
  for (const pg of curr.pings) {
    const [sx, sy] = toScreen(pg.x, pg.y);
    ctx.strokeStyle = C.gold;
    ctx.lineWidth = 2;
    ctx.beginPath(); ctx.arc(sx, sy, 4 + (now / 200) % 4, 0, Math.PI * 2); ctx.stroke();
  }
  for (const mate of curr.players) {
    if (mate.team === 0 || mate.team !== curr.me.team) continue;
    let [sx, sy] = toScreen(mate.x, mate.y);
    // A teammate beyond the radar's range is pinned to the rim, so you always know which way they are.
    const off = Math.hypot(sx - cx, sy - cy), far = off > r - 10;
    if (far) { sx = cx + ((sx - cx) / off) * (r - 10); sy = cy + ((sy - cy) / off) * (r - 10); }
    const color = mate.downed ? C.warn : C.good;
    // An arrow showing which way they face (or, at the rim, which way they lie).
    const ax = Math.cos(mate.aim), ay = Math.sin(mate.aim);
    const ang = far ? Math.atan2(sy - cy, sx - cx) : Math.atan2(-(ax * fx + ay * fy), -ax * fy + ay * fx);
    ctx.save();
    ctx.translate(sx, sy);
    ctx.rotate(ang + Math.PI / 2);
    glow(color, 8);
    ctx.fillStyle = color;
    ctx.beginPath(); ctx.moveTo(0, -8); ctx.lineTo(6, 6); ctx.lineTo(0, 3); ctx.lineTo(-6, 6); ctx.fill();
    noGlow();
    ctx.restore();
    ctx.fillStyle = C.text;
    ctx.font = font(9, 800);
    ctx.textAlign = 'center';
    ctx.fillText(mate.name.slice(0, 8), sx, sy + (sy > cy ? -10 : 17));
  }
  for (const b of curr.revealed) {
    const [sx, sy] = toScreen(b.x, b.y);
    ctx.fillStyle = C.bad;
    ctx.beginPath(); ctx.moveTo(sx, sy - 5); ctx.lineTo(sx + 5, sy); ctx.lineTo(sx, sy + 5); ctx.lineTo(sx - 5, sy); ctx.fill();
  }
  ctx.restore();

  // Rim, tick marks, and a north marker that slides around the edge.
  glow(C.main, 8);
  ctx.strokeStyle = C.main;
  ctx.lineWidth = 2;
  ctx.beginPath(); ctx.arc(cx, cy, r, 0, Math.PI * 2); ctx.stroke();
  noGlow();
  ctx.strokeStyle = C.dim;
  ctx.lineWidth = 1;
  for (let i = 0; i < 36; i++) {
    const a = (i / 36) * Math.PI * 2, len = i % 9 === 0 ? 7 : 3;
    ctx.beginPath();
    ctx.moveTo(cx + Math.cos(a) * (r + 2), cy + Math.sin(a) * (r + 2));
    ctx.lineTo(cx + Math.cos(a) * (r + 2 + len), cy + Math.sin(a) * (r + 2 + len));
    ctx.stroke();
  }
  ctx.fillStyle = C.main;
  ctx.font = font(11, 800);
  ctx.textAlign = 'center';
  ctx.fillText('N', cx - fx * (r + 16), cy + fy * (r + 16) + 4);
  // The player: an arrow at the centre, always pointing up.
  if (curr.me.alive) {
    glow(C.main, 8);
    ctx.fillStyle = C.text;
    ctx.beginPath(); ctx.moveTo(cx, cy - 7); ctx.lineTo(cx + 5, cy + 5); ctx.lineTo(cx, cy + 2); ctx.lineTo(cx - 5, cy + 5); ctx.fill();
    noGlow();
  }
  // Outside the zone: an arrow on the rim points the way back.
  const dz = Math.hypot(curr.zone.x - curr.me.x, curr.zone.y - curr.me.y);
  if (dz > curr.zone.r && curr.me.alive) {
    const a = Math.atan2(zy - cy, zx - cx);
    ctx.fillStyle = C.bad;
    ctx.beginPath();
    ctx.moveTo(cx + Math.cos(a) * (r - 4), cy + Math.sin(a) * (r - 4));
    ctx.lineTo(cx + Math.cos(a - 0.16) * (r - 16), cy + Math.sin(a - 0.16) * (r - 16));
    ctx.lineTo(cx + Math.cos(a + 0.16) * (r - 16), cy + Math.sin(a + 0.16) * (r - 16));
    ctx.fill();
    ctx.font = font(10);
    ctx.fillText('RETURN TO ZONE', cx, cy + r + 30);
  }
  ctx.fillStyle = C.dim;
  ctx.font = font(9);
  ctx.textAlign = 'center';
  ctx.fillText(touchActive ? 'TAP FOR MAP' : '[TAB] MAP', cx, cy + r + 18);
  if (pingMs && !REPLAY) {
    ctx.fillStyle = pingMs < 90 ? C.good : pingMs < 180 ? C.warn : C.bad;
    ctx.textAlign = 'left';
    ctx.fillText(`PING ${Math.round(pingMs)}ms`, cx + r - 14, cy - r + 2);
  }
}

// Full map: north up, the whole arena, everything seen so far.
function bigMapRect() {
  const size = Math.min(VW, VH) * 0.8;
  return { x: VW / 2 - size / 2, y: VH / 2 - size / 2 + 10, size };
}
function drawBigMap(now) {
  const { x, y, size } = bigMapRect();
  const k = size / 2000;
  panel(x - 14, y - 36, size + 28, size + 50, 16, C.dim, 'rgba(5,14,26,0.92)');
  ctx.fillStyle = C.main;
  ctx.font = font(13, 800);
  ctx.textAlign = 'left';
  ctx.fillText('TACTICAL MAP', x, y - 14);
  ctx.fillStyle = C.dim;
  ctx.font = font(10);
  ctx.textAlign = 'right';
  ctx.fillText(touchActive ? 'TAP MAP TO PING // TAP OUTSIDE TO CLOSE' : '[TAB] CLOSE // [Q] PING AHEAD', x + size, y - 14);
  ctx.save();
  ctx.beginPath(); ctx.rect(x, y, size, size); ctx.clip();
  ctx.fillStyle = 'rgba(4,14,26,0.9)';
  ctx.fillRect(x, y, size, size);
  ctx.strokeStyle = C.faint;
  ctx.lineWidth = 1;
  ctx.font = font(9);
  ctx.textAlign = 'left';
  for (let i = 0; i <= 8; i++) {
    const p = (i / 8) * size;
    ctx.beginPath(); ctx.moveTo(x + p, y); ctx.lineTo(x + p, y + size); ctx.moveTo(x, y + p); ctx.lineTo(x + size, y + p); ctx.stroke();
    if (i < 8) { ctx.fillStyle = C.dim; ctx.fillText('ABCDEFGH'[i], x + p + 4, y + 11); ctx.fillText(String(i + 1), x + 4, y + p + 22); }
  }
  for (const l of world.lakes) {
    ctx.fillStyle = 'rgba(47,155,230,0.5)';
    ctx.beginPath(); ctx.arc(x + l.x * k, y + l.y * k, l.r * k, 0, Math.PI * 2); ctx.fill();
  }
  for (const b of world.bushes) {
    ctx.fillStyle = 'rgba(125,255,166,0.3)';
    ctx.beginPath(); ctx.arc(x + b.x * k, y + b.y * k, Math.max(2, b.r * k), 0, Math.PI * 2); ctx.fill();
  }
  for (const o of knownObstacles.values()) {
    if (o.k === 'tree') {
      ctx.fillStyle = 'rgba(125,255,166,0.5)';
      ctx.fillRect(x + o.x * k, y + o.y * k, 2.5, 2.5);
    } else {
      ctx.fillStyle = 'rgba(111,243,255,0.22)';
      ctx.fillRect(x + o.x * k, y + o.y * k, o.w * k, o.h * k);
      ctx.strokeStyle = C.dim;
      ctx.strokeRect(x + o.x * k, y + o.y * k, o.w * k, o.h * k);
    }
  }
  glow(C.bad, 8);
  ctx.strokeStyle = C.bad;
  ctx.lineWidth = 2;
  ctx.beginPath(); ctx.arc(x + curr.zone.x * k, y + curr.zone.y * k, curr.zone.r * k, 0, Math.PI * 2); ctx.stroke();
  noGlow();
  for (const pg of curr.pings) {
    ctx.strokeStyle = C.gold;
    ctx.beginPath(); ctx.arc(x + pg.x * k, y + pg.y * k, 5 + (now / 200) % 5, 0, Math.PI * 2); ctx.stroke();
  }
  for (const mate of curr.players) {
    if (mate.team === 0 || mate.team !== curr.me.team) continue;
    const color = mate.downed ? C.warn : C.good;
    ctx.save();
    ctx.translate(x + mate.x * k, y + mate.y * k);
    ctx.rotate(mate.aim + Math.PI / 2);
    glow(color, 10);
    ctx.fillStyle = color;
    ctx.beginPath(); ctx.moveTo(0, -10); ctx.lineTo(7, 7); ctx.lineTo(0, 3); ctx.lineTo(-7, 7); ctx.fill();
    noGlow();
    ctx.restore();
    ctx.fillStyle = C.text;
    ctx.font = font(11, 800);
    ctx.textAlign = 'left';
    ctx.fillText(`${mate.name}${mate.downed ? ' // DOWN' : ''}`, x + mate.x * k + 11, y + mate.y * k + 4);
  }
  for (const b of curr.revealed) {
    ctx.fillStyle = C.bad;
    ctx.beginPath(); ctx.arc(x + b.x * k, y + b.y * k, 5, 0, Math.PI * 2); ctx.fill();
  }
  // The player: an arrow turned to the facing direction.
  ctx.translate(x + curr.me.x * k, y + curr.me.y * k);
  ctx.rotate(yaw + Math.PI / 2);
  glow(C.main, 10);
  ctx.fillStyle = C.text;
  ctx.beginPath(); ctx.moveTo(0, -9); ctx.lineTo(6, 7); ctx.lineTo(0, 3); ctx.lineTo(-6, 7); ctx.fill();
  noGlow();
  ctx.restore();
}

// ---------- Shop ----------
let shopOpen = false;
let shopRows = []; // touch targets: { slot, x, y, w, h }
const WEAPON_NAMES = ['Pistol', 'SMG', 'Rifle', 'Shotgun', 'Sniper', 'Armour', 'Grenade'];
const WEAPON_NOTES = ['semi-auto sidearm', 'fast, close range', 'all-round automatic', '8 pellets, very close', 'one-shot headshots, right-click to scope',
  'halves body damage while it lasts', 'throw with G; carry up to 2'];
function drawShop() {
  const a = curr.ammo;
  const w = Math.min(460, VW - 24), rowH = 40, h = 86 + rowH * 7;
  const x = VW / 2 - w / 2, y = Math.max(70, VH / 2 - h / 2);
  panel(x, y, w, h, 16, C.dim, 'rgba(5,14,26,0.92)');
  ctx.textAlign = 'left';
  ctx.fillStyle = C.main;
  ctx.font = font(15, 800);
  ctx.fillText('ARMORY', x + 18, y + 28);
  ctx.textAlign = 'right';
  ctx.fillStyle = C.gold;
  ctx.fillText(`CR ${a.credits}`, x + w - 18, y + 28);
  shopRows = [];
  for (let i = 0; i < 7; i++) {
    const ry = y + 42 + i * rowH;
    const gear = i >= 5;
    const owned = gear ? false : a.owned[i];
    const full = i === 5 ? a.armor >= 50 : i === 6 ? a.grenades >= 2 : false;
    const price = gear ? a.gearPrices[i - 5] : owned ? a.ammoPrice : a.prices[i];
    const can = a.credits >= price;
    ctx.fillStyle = a.slot === i + 1 ? 'rgba(111,243,255,0.16)' : 'rgba(255,255,255,0.04)';
    ctx.fillRect(x + 12, ry, w - 24, rowH - 6);
    ctx.textAlign = 'left';
    ctx.fillStyle = C.main;
    ctx.font = font(13, 800);
    ctx.fillText(`[${i + 1}]`, x + 22, ry + 24);
    ctx.fillStyle = C.text;
    ctx.fillText(WEAPON_NAMES[i].toUpperCase(), x + 62, ry + 18);
    ctx.fillStyle = C.dim;
    ctx.font = font(10);
    ctx.fillText(WEAPON_NOTES[i], x + 62, ry + 32);
    ctx.textAlign = 'right';
    ctx.font = font(12, 800);
    ctx.fillStyle = owned || full ? C.good : can ? C.gold : C.bad;
    ctx.fillText(full ? 'FULL' : owned ? `OWNED // AMMO ${price}` : i === 5 ? `${price} CR // ${a.armor}/50` : i === 6 ? `${price} CR // ${a.grenades}/2` : `${price} CR`, x + w - 22, ry + 24);
    shopRows.push({ slot: i + 1, x: x + 12, y: ry, w: w - 24, h: rowH - 6 });
  }
  ctx.textAlign = 'center';
  ctx.fillStyle = C.dim;
  ctx.font = font(10);
  ctx.fillText(touchActive ? 'TAP A ROW TO BUY // TAP OUTSIDE TO CLOSE' : 'PRESS 1-7 TO BUY // [B] CLOSE // KILLS +300 CR',
    VW / 2, y + h - 14);
}

function drawButton(b, label, color, alpha) {
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.fillStyle = 'rgba(6,16,30,0.75)';
  ctx.beginPath(); ctx.arc(b.x, b.y, b.r, 0, Math.PI * 2); ctx.fill();
  ctx.globalAlpha = Math.min(1, alpha + 0.4);
  ctx.strokeStyle = color;
  ctx.lineWidth = 2.5;
  ctx.beginPath(); ctx.arc(b.x, b.y, b.r - 2, 0, Math.PI * 2); ctx.stroke();
  ctx.fillStyle = color;
  ctx.font = font(b.r > 40 ? 14 : 11, 800);
  ctx.textAlign = 'center';
  ctx.fillText(label, b.x, b.y + 4);
  ctx.restore();
}

function drawTouchControls() {
  ctx.save();
  ctx.strokeStyle = C.dim;
  ctx.lineWidth = 2;
  ctx.fillStyle = 'rgba(6,16,30,0.45)';
  ctx.beginPath(); ctx.arc(STICK.x, STICK.y, STICK.r, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
  ctx.fillStyle = 'rgba(111,243,255,0.5)';
  ctx.beginPath(); ctx.arc(STICK.x + stick.dx * STICK.r, STICK.y + stick.dy * STICK.r, 26, 0, Math.PI * 2); ctx.fill();
  ctx.restore();
  drawButton(FIRE, 'FIRE', C.bad, fireHeld ? 0.9 : 0.5);
  drawButton(RELOAD, 'RELOAD', C.warn, 0.5);
  drawButton(SWAP, 'SWAP', C.main, 0.5);
  drawButton(REVIVE_BTN, 'REVIVE', C.good, reviveTouched ? 0.9 : 0.5);
  drawButton(SHOP_BTN, 'SHOP', C.gold, shopOpen ? 0.9 : 0.5);
  drawButton(JUMP_BTN, 'JUMP', C.main, 0.5);
  drawButton(CROUCH_BTN, 'CROUCH', C.main, crouchToggle ? 0.9 : 0.5);
  drawButton(NADE_BTN, `NADE ${curr.ammo.grenades}`, C.warn, curr.ammo.grenades ? 0.6 : 0.25);
  if (curr.ammo.weapon === 'Sniper') drawButton(SCOPE_BTN, 'SCOPE', C.main, scoped ? 0.9 : 0.5);
  drawButton(CHAT_BTN, 'TEAM', C.main, chatWheel === 'team' ? 0.9 : 0.5);
  drawButton(NEAR_BTN, 'NEAR', C.gold, chatWheel === 'near' ? 0.9 : 0.5);
  drawButton(MUTE_BTN, voiceDeaf ? 'UNMUTE' : 'MUTE', C.main, voiceDeaf ? 0.9 : 0.5);
  drawButton(REPORT_BTN, 'REPORT', C.warn, 0.5);
}

// The sniper scope: a round window with fine cross lines; everything outside it is dark.
function drawScope() {
  const cx = VW / 2, cy = VH / 2, r = Math.min(VW, VH) * 0.42;
  ctx.fillStyle = 'rgba(2,6,12,0.94)';
  ctx.beginPath();
  ctx.rect(0, 0, VW, VH);
  ctx.arc(cx, cy, r, 0, Math.PI * 2, true);
  ctx.fill();
  ctx.strokeStyle = 'rgba(10,20,30,0.9)';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.moveTo(cx - r, cy); ctx.lineTo(cx - 6, cy); ctx.moveTo(cx + 6, cy); ctx.lineTo(cx + r, cy);
  ctx.moveTo(cx, cy - r); ctx.lineTo(cx, cy - 6); ctx.moveTo(cx, cy + 6); ctx.lineTo(cx, cy + r);
  ctx.stroke();
  ctx.fillStyle = C.bad;
  ctx.fillRect(cx - 1, cy - 1, 2, 2);
  ctx.strokeStyle = C.main;
  ctx.lineWidth = 2;
  ctx.beginPath(); ctx.arc(cx, cy, r, 0, Math.PI * 2); ctx.stroke();
}

// ---------- Controls screen ----------
// Shown the first time someone plays, and whenever they press H or the ? button.
let helpOpen = false;
try { if (!REPLAY && !localStorage.getItem('arena-seen-help')) helpOpen = true; } catch { /* storage unavailable */ }
function closeHelp() {
  helpOpen = false;
  try { localStorage.setItem('arena-seen-help', '1'); } catch { /* storage unavailable */ }
}
document.getElementById('help')?.addEventListener('click', () => { if (helpOpen) closeHelp(); else helpOpen = true; });
// ---------- Voice ----------
// Hold T to talk to your team (heard from anywhere), hold Y to talk to nearby players.
// The microphone is encoded with Opus in the browser and relayed by the server (see voice.go).
// Needs Chromium-based browsers, which provide MediaStreamTrackProcessor and WebCodecs.
const voiceSupported = typeof MediaStreamTrackProcessor !== 'undefined' && typeof AudioEncoder !== 'undefined'
  && typeof AudioDecoder !== 'undefined' && typeof EncodedAudioChunk !== 'undefined';
const VOICE_RANGE = 900;   // matches hearRange on the server
const VOICE_FLAG_TEAM = 1, VOICE_FLAG_MUFFLE = 2;
let voiceMode = null;       // 0 = team, 1 = nearby, while the key is held
let micStream = null, micReader = null, voiceEnc = null;
const voiceDecoders = new Map(), voiceNext = new Map(), voiceFlags = new Map();
let voiceDeaf = false;     // K: stop hearing other players' voice; sound effects are unaffected
let lastSpeaker = null;   // { id, at } of the last voice heard, for reporting

function toggleVoiceDeaf() {
  voiceDeaf = !voiceDeaf;
  hearChat([{ from: 'Voice', team: false, text: voiceDeaf ? 'muted' : 'unmuted' }]);
}
// O reports the last voice heard in the last 30 seconds as abusive. The server keeps no audio.
async function reportLastSpeaker() {
  if (!lastSpeaker || performance.now() - lastSpeaker.at > 30000 || !id) {
    hearChat([{ from: 'Voice', team: false, text: 'no voice to report' }]);
    return;
  }
  const body = new URLSearchParams({ speaker: String(lastSpeaker.id) });
  const res = await fetch(`/matches/${id}/voice-report`, { method: 'POST', body, credentials: 'same-origin' });
  hearChat([{ from: 'Voice', team: false, text: res.ok ? 'report sent' : 'report failed' }]);
}

async function startTalk(team) {
  if (!voiceSupported || !ws || voiceMode !== null || !curr?.started) return;
  if (!navigator.mediaDevices?.getUserMedia) return;
  ensureAudio();
  voiceMode = team ? 0 : 1;
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } });
  } catch { voiceMode = null; return; }
  if (voiceMode === null) { stream.getTracks().forEach(t => t.stop()); return; } // key released while asking
  micStream = stream;
  micReader = new MediaStreamTrackProcessor({ track: stream.getAudioTracks()[0] }).readable.getReader();
  voiceEnc = new AudioEncoder({
    output: chunk => {
      if (voiceMode === null || ws.readyState !== WebSocket.OPEN) return;
      const pkt = new Uint8Array(chunk.byteLength);
      chunk.copyTo(pkt);
      const frame = new Uint8Array(pkt.length + 1);
      frame[0] = voiceMode;
      frame.set(pkt, 1);
      ws.send(frame.buffer);
    },
    error: () => stopTalk(),
  });
  voiceEnc.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: 1, bitrate: 24000 });
  pumpMic();
}
async function pumpMic() {
  while (voiceMode !== null && micReader) {
    const { value, done } = await micReader.read();
    if (done) break;
    if (voiceEnc && voiceEnc.state === 'configured') voiceEnc.encode(value);
    value.close();
  }
}
function stopTalk() {
  voiceMode = null;
  micReader?.cancel().catch(() => {});
  micReader = null;
  micStream?.getTracks().forEach(t => t.stop());
  micStream = null;
  try { voiceEnc?.close(); } catch { /* already closed */ }
  voiceEnc = null;
}
// A relayed frame: kind (1), flags, sender ID (two bytes), then the Opus packet.
function onVoice(buf) {
  const u = new Uint8Array(buf);
  if (u.length < 5 || u[0] !== 1 || !audioCtx) return;
  const id = (u[2] << 8) | u[3];
  let dec = voiceDecoders.get(id);
  if (!dec) {
    dec = new AudioDecoder({
      output: d => playVoice(id, d),
      error: () => { voiceDecoders.delete(id); },
    });
    dec.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: 1 });
    voiceDecoders.set(id, dec);
  }
  voiceFlags.set(id, u[1]);
  lastSpeaker = { id, at: performance.now() };
  dec.decode(new EncodedAudioChunk({ type: 'key', timestamp: performance.now() * 1000, data: u.subarray(4) }));
}
function playVoice(id, d) {
  const n = d.numberOfFrames, pcm = new Float32Array(n);
  d.copyTo(pcm, { planeIndex: 0 });
  d.close();
  if (!audioCtx || muted || voiceDeaf) return;
  const flags = voiceFlags.get(id) || 0;
  let gain = 1.2, pan = 0;
  if (!(flags & VOICE_FLAG_TEAM)) {
    // Nearby voice fades and pans with distance, like other sounds.
    const p = curr?.players?.find(q => q.id === id);
    if (!p) return;
    const dx = p.x - camera.position.x, dy = p.y - camera.position.z, dist = Math.hypot(dx, dy);
    if (dist > VOICE_RANGE) return;
    gain *= Math.pow(1 - dist / VOICE_RANGE, 1.6);
    pan = dist < 1 ? 0 : clamp((dx * -Math.sin(yaw) + dy * Math.cos(yaw)) / dist, -1, 1) * 0.9;
  }
  const buf = audioCtx.createBuffer(1, n, 48000);
  buf.copyToChannel(pcm, 0);
  const src = audioCtx.createBufferSource();
  src.buffer = buf;
  let node = src;
  if (flags & VOICE_FLAG_MUFFLE) { // behind cover: duller and quieter
    const lp = audioCtx.createBiquadFilter();
    lp.type = 'lowpass';
    lp.frequency.value = 700;
    node.connect(lp);
    node = lp;
    gain *= 0.6;
  }
  const g = audioCtx.createGain();
  g.gain.value = gain;
  const sp = audioCtx.createStereoPanner();
  sp.pan.value = pan;
  node.connect(g).connect(sp).connect(audioCtx.destination);
  // Chain each speaker's packets so they play back to back; restart if the queue runs too far behind.
  let t = Math.max(audioCtx.currentTime, voiceNext.get(id) || 0);
  if (t - audioCtx.currentTime > 0.3) t = audioCtx.currentTime;
  src.start(t);
  voiceNext.set(id, t + buf.duration);
}

// ---------- Capture and plant-and-defuse markers ----------
// Rings on the ground: a zone is coloured by its owner, and the planted site turns red.
const zoneRings = new Map(), siteRings = new Map();
const OWNER_RING = [0x9aa7b8, 0x4aa8ff, 0xff6b4a];
function groundRing(inner, outer, x, y) {
  const mat = new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.65, side: THREE.DoubleSide, depthWrite: false });
  const mesh = new THREE.Mesh(new THREE.RingGeometry(inner, outer, 56).rotateX(-Math.PI / 2), mat);
  mesh.position.set(x, 0.4, y);
  scene.add(mesh);
  return mesh;
}
function updateCaptureMarkers(c) {
  for (const z of zoneRings.values()) z.visible = !!c;
  if (!c) return;
  for (const z of c.zones) {
    if (!zoneRings.has(z.name)) zoneRings.set(z.name, groundRing(84, 90, z.x, z.y)); // captureRadius is 90
    const ring = zoneRings.get(z.name);
    ring.material.color.setHex(OWNER_RING[z.owner]);
    ring.visible = true;
  }
}
const siteTags = new Map(); // site name -> { sprite, text }
// siteTagText is the label over a site: planting progress, the fuse, or defuse progress.
function siteTagText(b, i) {
  const site = b.sites[i];
  if (b.planted && b.site === i + 1) {
    if (b.defusing > 0) return `${site.name} DEFUSING ${Math.round(100 * b.defusing / BOMB_DEFUSE_TIME)}% · ${Math.ceil(b.fuse)}s`;
    return `${site.name} BOMB ${Math.ceil(b.fuse)}s`;
  }
  if (b.plantSite === i + 1) return `${site.name} PLANTING ${Math.round(100 * b.planting / BOMB_PLANT_TIME)}%`;
  return site.name;
}
function updateBombMarkers(b) {
  for (const r of siteRings.values()) r.visible = !!b;
  for (const t of siteTags.values()) t.sprite.visible = !!b;
  if (!b) return;
  b.sites.forEach((site, i) => {
    if (!siteRings.has(site.name)) siteRings.set(site.name, groundRing(54, 60, site.x, site.y)); // bombRadius is 60
    const ring = siteRings.get(site.name);
    const hot = b.planted && b.site === i + 1;
    ring.material.color.setHex(hot ? 0xff3b3b : 0xffc247);
    ring.visible = !b.defused && !b.exploded || hot;

    // Redraw the label only when its text changes.
    const text = siteTagText(b, i);
    let tag = siteTags.get(site.name);
    if (!tag || tag.text !== text) {
      if (tag) { scene.remove(tag.sprite); tag.sprite.material.map.dispose(); tag.sprite.material.dispose(); }
      const sprite = nameTag(text, hot ? '#ff6b6b' : '#ffd36b');
      sprite.position.set(site.x, 90, site.y);
      scene.add(sprite);
      tag = { sprite, text };
      siteTags.set(site.name, tag);
    }
    tag.sprite.visible = !b.defused && !b.exploded || hot;
  });
}

// ---------- Capture and plant-and-defuse status ----------
// Constants match capture.go and bomb.go.
const CAPTURE_TIME = 4, BOMB_PLANT_TIME = 1.5, BOMB_DEFUSE_TIME = 5;
const OWNER_NAMES = ['none', 'Alpha', 'Bravo'];
// renderObjectives shows the capture score and each zone's owner and progress, or hides the panel.
function renderObjectives(c) {
  const panel = document.getElementById('objectives');
  if (!panel) return;
  panel.hidden = !c;
  if (!c) return;
  const row = (left, right, cls) => {
    const el = document.createElement('div');
    el.className = cls ? `row ${cls}` : 'row';
    const a = document.createElement('span'), b = document.createElement('span');
    a.textContent = left;
    b.textContent = right;
    el.append(a, b);
    return el;
  };
  const rows = [row(`Alpha ${c.scores[0]}`, `${c.scores[1]} Bravo`, 'score')];
  for (const z of c.zones) {
    const taking = z.team && z.progress > 0 ? `${Math.round(100 * z.progress / CAPTURE_TIME)}%` : '';
    rows.push(row(z.name, `${OWNER_NAMES[z.owner]}${taking ? ' · ' + taking : ''}`));
  }
  if (c.winner) rows.push(row('', `${OWNER_NAMES[c.winner]} win`));
  panel.replaceChildren(...rows);
}
function captureText(c) {
  const zones = c.zones.map(z => {
    const taking = z.team && z.progress > 0 ? ` ${Math.round(100 * z.progress / CAPTURE_TIME)}%` : '';
    return `${z.name} ${OWNER_NAMES[z.owner]}${taking}`;
  }).join(' · ');
  const won = c.winner ? ` · ${OWNER_NAMES[c.winner]} win` : '';
  return `Capture · Alpha ${c.scores[0]} : ${c.scores[1]} Bravo · ${zones}${won}`;
}
function bombText(b) {
  if (b.exploded) return 'Bomb exploded';
  if (b.defused) return 'Bomb defused';
  if (b.planted && b.defusing > 0) return `Defusing ${Math.round(100 * b.defusing / BOMB_DEFUSE_TIME)}% · ${Math.ceil(b.fuse)}s`;
  if (b.planted) return `Bomb at ${b.sites[b.site - 1].name} · ${Math.ceil(b.fuse)}s`;
  if (b.plantSite) return `Planting ${b.sites[b.plantSite - 1].name} ${Math.round(100 * b.planting / BOMB_PLANT_TIME)}%`;
  return 'Bomb not planted';
}

// ---------- Quick chat ----------
// V opens the team wheel, X the nearby wheel. Press 1-8 to send a preset; Esc or the same key closes it.
// The list must match quickChats in chat.go.
const CHAT_PRESETS = ['Enemy spotted', 'Need help', 'On my way', 'Hold this position',
  'Nice shot', 'Going for loot', 'Cover me', 'Thanks'];
let chatWheel = null; // 'team', 'near', or null when closed
const chatFeed = []; // messages heard recently: { from, team, text, at }
const CHAT_SHOW_MS = 5000;

function toggleChatWheel(kind) { chatWheel = chatWheel === kind ? null : kind; }
function sendChat(index) {
  sendToServer(JSON.stringify({ t: 'chat', k: index, team: chatWheel === 'team' }));
  chatWheel = null;
}
function hearChat(list) {
  const now = performance.now();
  for (const c of list) chatFeed.push({ ...c, at: now });
  while (chatFeed.length > 5) chatFeed.shift();
}
let chatRows = []; // touch targets for the open wheel: { slot, x, y, w, h }
function drawChat() {
  const now = performance.now();
  chatRows = [];
  while (chatFeed.length && now - chatFeed[0].at > CHAT_SHOW_MS) chatFeed.shift();
  ctx.textAlign = 'left';
  chatFeed.forEach((c, i) => {
    const y = VH - 170 - (chatFeed.length - 1 - i) * 20;
    ctx.font = font(12, 700);
    ctx.fillStyle = c.team ? C.main : C.gold;
    ctx.fillText((c.team ? '[team] ' : '[near] ') + c.from + ': ' + c.text, 18, y);
  });
  if (!chatWheel) return;
  const w = Math.min(320, VW - 24), rowH = 26, h = 48 + CHAT_PRESETS.length * rowH;
  const x = VW / 2 - w / 2, y = Math.max(60, VH / 2 - h / 2);
  panel(x, y, w, h, 14, C.dim, 'rgba(5,14,26,0.94)');
  ctx.fillStyle = C.main;
  ctx.font = font(13, 800);
  ctx.fillText(chatWheel === 'team' ? 'TEAM CALL' : 'NEARBY CALL', x + 18, y + 26);
  CHAT_PRESETS.forEach((text, i) => {
    const ry = y + 48 + i * rowH;
    chatRows.push({ slot: i, x, y: ry - rowH + 6, w, h: rowH });
    ctx.fillStyle = C.gold;
    ctx.font = font(12, 800);
    ctx.fillText(String(i + 1), x + 18, ry);
    ctx.fillStyle = C.text;
    ctx.font = font(12);
    ctx.fillText(text, x + 42, ry);
  });
}

const HELP_KEYS = [
  ['Move', 'W A S D'], ['Look', 'Mouse'], ['Fire', 'Left click or F'], ['Scope (sniper)', 'Right click or Z'],
  ['Jump', 'Space'], ['Crouch', 'Hold C or Ctrl'], ['Reload', 'R'], ['Switch weapon', '1-5 or wheel'],
  ['Shop', 'B, then 1-7'], ['Grenade', 'G'], ['Revive teammate', 'Hold E'], ['Ping', 'Q'],
  ['Quick chat', 'V team, X nearby, then 1-8'], ['Team talk', 'Hold T'], ['Nearby talk', 'Hold Y'], ['Mute voice', 'K'], ['Report voice', 'O'], ['Full map', 'Tab or N'], ['Mute', 'M'], ['This screen', 'H'],
];
const HELP_TOUCH = [
  ['Move', 'Left stick'], ['Look', 'Drag anywhere'], ['Fire, reload, swap', 'Right buttons'],
  ['Jump, crouch', 'Middle buttons'], ['Shop, grenade', 'Top right buttons'], ['Full map', 'Tap the radar'],
  ['Ping', 'Tap the full map'], ['Mute, report voice', 'Bottom right, above TEAM'], ['This screen', '? at the top'],
];
const HELP_TIPS = [
  'Aim at the head: a centred shot there does the most damage.',
  'Crouch behind small crates. Big crates and containers hide you standing.',
  'A bush hides you from a distance until you fire. You cannot shoot while swimming.',
  'Stay inside the red circle. Coins and eliminations pay for better weapons.',
];
function drawHelp() {
  const rows = touchActive ? HELP_TOUCH : HELP_KEYS;
  const w = Math.min(520, VW - 24), rowH = 22, h = 96 + rows.length * rowH + HELP_TIPS.length * 18;
  const x = VW / 2 - w / 2, y = Math.max(60, VH / 2 - h / 2);
  panel(x, y, w, h, 16, C.dim, 'rgba(5,14,26,0.94)');
  ctx.textAlign = 'left';
  ctx.fillStyle = C.main;
  ctx.font = font(15, 800);
  ctx.fillText('HOW TO PLAY', x + 18, y + 28);
  rows.forEach(([what, how], i) => {
    const ry = y + 54 + i * rowH;
    ctx.textAlign = 'left';
    ctx.fillStyle = C.text;
    ctx.font = font(12);
    ctx.fillText(what, x + 22, ry);
    ctx.textAlign = 'right';
    ctx.fillStyle = C.gold;
    ctx.font = font(12, 800);
    ctx.fillText(how, x + w - 22, ry);
  });
  ctx.textAlign = 'left';
  ctx.fillStyle = C.dim;
  ctx.font = font(11);
  HELP_TIPS.forEach((tip, i) => ctx.fillText('> ' + tip, x + 22, y + 62 + rows.length * rowH + i * 18));
  ctx.textAlign = 'center';
  ctx.fillStyle = C.main;
  ctx.fillText(touchActive ? 'TAP ANYWHERE TO CLOSE' : 'PRESS H OR ESC TO CLOSE', VW / 2, y + h - 12);
}

function drawFeed() {
  ctx.textAlign = 'right';
  ctx.font = font(11);
  const lines = curr.events.slice(-6);
  lines.forEach((line, i) => {
    const w = ctx.measureText(line).width + 20, y = 14 + i * 22;
    ctx.fillStyle = C.bg;
    ctx.fillRect(VW - 14 - w, y, w, 18);
    ctx.fillStyle = line.includes(window.MATCH.name) ? C.gold : C.main;
    ctx.fillRect(VW - 16, y, 2, 18);
    ctx.fillStyle = C.text;
    ctx.fillText(line, VW - 24, y + 13);
  });
}

function drawOverlay(now) {
  ctx.setTransform(DPR, 0, 0, DPR, 0, 0);
  ctx.clearRect(0, 0, VW, VH);
  if (!curr) return;
  rememberObstacles();

  drawFeed();
  if (curr.me.alive) {
    if (camera.fov < 40 && !REPLAY) drawScope();
    if (!shopOpen && !bigMap && !REPLAY) { if (camera.fov >= 40) drawCrosshair(curr.me.bloom); drawHitMarker(now); }
    if (!REPLAY) drawAmmo();
    drawHealth();
  }
  drawCompass();
  drawRadar(now);
  drawSquad();
  if (touchActive) drawTouchControls();
  if (bigMap) drawBigMap(now);
  if (shopOpen && curr.me.alive) drawShop(); else shopOpen = false;
  if (helpOpen) drawHelp();
  drawChat();

  ctx.textAlign = 'center';
  if (curr.started && curr.me.downed && !curr.over) {
    glow(C.bad, 12);
    ctx.fillStyle = C.bad;
    ctx.font = font(22, 800);
    ctx.fillText('DOWNED // TEAMMATE CAN REVIVE YOU', VW / 2, VH / 2 + 70);
    noGlow();
  }
  if (curr.started && !curr.me.alive && !curr.over && !bigMap && !REPLAY) {
    glow(C.bad, 12);
    ctx.fillStyle = C.text;
    ctx.font = font(30, 800);
    ctx.fillText(curr.round ? 'ELIMINATED // BACK NEXT ROUND' : `ELIMINATED // #${curr.me.place}`, VW / 2, VH / 2);
    noGlow();
  }
  // Rounds mode banners: the buy countdown, and who took the round.
  if (curr.round && !curr.over) {
    const r = curr.round;
    if (r.phase === 'buy' && !shopOpen) {
      ctx.fillStyle = C.gold;
      ctx.font = font(18, 800);
      ctx.fillText(`BUY PHASE // ${Math.ceil(r.left)}s // ${touchActive ? 'TAP SHOP' : 'PRESS B'}`, VW / 2, 120);
    }
    if (r.phase === 'end') {
      const last = curr.events[curr.events.length - 1] ?? '';
      const won = last.includes(`Team ${r.team} wins`), draw = last.includes('draw');
      glow(won ? C.good : draw ? C.main : C.bad, 14);
      ctx.fillStyle = won ? C.good : draw ? C.text : C.bad;
      ctx.font = font(34, 800);
      ctx.fillText(draw ? 'ROUND DRAWN' : won ? 'ROUND WON' : 'ROUND LOST', VW / 2, VH / 2 - 60);
      noGlow();
    }
  }
  if (curr.over) {
    ctx.fillStyle = 'rgba(4,12,24,0.78)';
    ctx.fillRect(0, VH / 2 - 76, VW, 160);
    ctx.fillStyle = C.main;
    ctx.fillRect(0, VH / 2 - 76, VW, 2);
    ctx.fillRect(0, VH / 2 + 82, VW, 2);
    const won = curr.me.place === 1 && !REPLAY;
    glow(won ? C.gold : C.main, 16);
    ctx.fillStyle = won ? C.gold : C.text;
    ctx.font = font(38, 800);
    ctx.fillText(won ? 'VICTORY' : `${curr.winner.toUpperCase()} WINS`, VW / 2, VH / 2 - 8);
    noGlow();
    ctx.fillStyle = C.text;
    ctx.font = font(15);
    ctx.fillText(REPLAY ? `BOT MATCH // ${Math.round(R.duration)}s // ${R.players} PLAYERS`
      : curr.round ? `ROUNDS ${curr.round.mine} : ${curr.round.theirs} // ${curr.me.kills} ELIMINATIONS`
      : `PLACED #${curr.me.place} // ${curr.me.kills} ELIMINATIONS`, VW / 2, VH / 2 + 26);
    ctx.fillStyle = C.good;
    ctx.font = font(18, 800);
    const cups = curr.ranked ? `   ${curr.trophies >= 0 ? '+' : ''}${curr.trophies} TROPHIES` : '   UNRANKED ROOM';
    if (!REPLAY) ctx.fillText(`+${curr.xp} XP${cups}`, VW / 2, VH / 2 + 58);
  }
}

// ---------- Main loop ----------
let lastFrame = performance.now();
let fxOn = true;
let renderOn = true;
function frame(now) {
  requestAnimationFrame(frame);
  const dt = Math.min(0.1, (now - lastFrame) / 1000);
  lastFrame = now;
  if (!REPLAY) { renderFrame(now, dt, 0); return; }
  if (CAPTURE) return; // frames are drawn by window.__replay.step
  vclock += dt * 1000;
  renderFrame(vclock, dt, replayPaused ? 0 : dt * 1000 * replaySpeed);
}

// renderFrame draws one frame. now is the animation clock in milliseconds and dt the seconds since
// the last frame; gameMs is how far a replay moves forward (unused in a live match).
function renderFrame(now, dt, gameMs) {
  for (let i = tracers.length - 1; i >= 0; i--) {
    if (now > tracers[i].until) {
      scene.remove(tracers[i].obj);
      tracers[i].obj.geometry.dispose();
      tracers.splice(i, 1);
    }
  }

  let t = 0;
  if (REPLAY) {
    if (R) t = replayAdvance(gameMs, now);
  } else if (curr) {
    t = Math.min(1, (now - currAt) / TICK_MS);
  }
  if (curr) {
    let meX = lerp(prev.me.x, curr.me.x, t);
    let meY = lerp(prev.me.y, curr.me.y, t);
    if (!REPLAY && predicting && canMove()) {
      const p = shownPosition(dt, now);
      meX = p.x;
      meY = p.y;
    }
    syncPlayers(t, dt, now);
    syncObstacles();
    syncLoot(now);
    syncBeacons();
    syncPings();
    syncGrenades(now);
    updateWorld(meX, meY);
    updateCamera(meX, meY, dt);
    updateViewmodel(now);
  }

  if (!renderOn) return; // a test has switched drawing off to keep timers accurate
  if (composer && fxOn) composer.render(); else renderer.render(scene, camera);
  drawOverlay(now);
}

// ---------- Replay: spectating a recorded match ----------
// The recording holds the whole world ten times a second. Playback builds the same snapshot shape
// the live game uses, seen from whichever player the camera follows.
let R = null;            // the loaded recording
let replayTime = 0;      // milliseconds of game time shown
let replaySpeed = 1;
let replayPaused = false;
let followId = 0;
let autoFollow = true;   // the camera moves to wherever the action is
let followSince = 0;     // replayTime of the last camera switch
let camYaw = 0;
let camBack = 78;        // how far behind the player the camera sits
let shotsDoneTo = -1;    // last frame whose shots have been drawn
const lastShotBy = new Map(); // player id -> replayTime of their latest shot
const snapCache = { i: -1, follow: -1, snap: null };

async function loadReplay() {
  hud.textContent = 'Loading replay…';
  const res = await fetch(`/replays/${id}/data`);
  if (!res.ok) { hud.textContent = 'This replay is no longer kept. Record a new one from the lobby.'; return; }
  const rep = await res.json();
  // Carry loot and obstacles forward into frames where they did not change.
  let loot = [], obstacles = [];
  for (const f of rep.frames) {
    loot = f.loot ?? loot;
    obstacles = f.obstacles ?? obstacles;
    f.loot = loot;
    f.obstacles = obstacles;
  }
  if (rep.world) buildWorld(rep.world);
  R = rep;
  followId = rep.frames[0].players[0].id;
  camYaw = rep.frames[0].players[0].aim;
  window.__replay.ready = true;
  window.__replay.duration = rep.duration * 1000;
}

const FAKE_AMMO = { weapon: 'Pistol', slot: 1, mag: 0, reserve: 0, reloading: false, reloadFrac: 0, auto: false, range: 0,
  armor: 0, grenades: 0, gearPrices: [0, 0],
  credits: 0, owned: [true, false, false, false, false], prices: [0, 0, 0, 0, 0], ammoPrice: 0 };

function replaySnap(i) {
  i = clamp(i, 0, R.frames.length - 1);
  if (snapCache.i === i && snapCache.follow === followId) return snapCache.snap;
  const f = R.frames[i];
  const alive = f.players.filter(p => p.alive);
  const me = alive.find(p => p.id === followId) ?? f.players.find(p => p.id === followId) ?? alive[0] ?? f.players[0];
  const snap = {
    t: 'state', started: true, over: R.finished && i === R.frames.length - 1, winner: R.winner, alive: alive.length,
    zone: f.zone, me, players: alive, loot: f.loot, obstacles: f.obstacles, events: f.events,
    revealed: [], pings: [], hitSeq: 0, hitKind: '', ammo: FAKE_AMMO, xp: 0, trophies: 0, ranked: false,
    round: f.round ? { ...f.round, team: 'Alpha' } : undefined,
  };
  snapCache.i = i; snapCache.follow = followId; snapCache.snap = snap;
  return snap;
}

function switchFollow(dir) {
  const f = R.frames[clamp(Math.floor(replayTime / R.frameMs), 0, R.frames.length - 1)];
  const alive = f.players.filter(p => p.alive).sort((a, b) => a.id - b.id);
  if (!alive.length) return;
  const at = alive.findIndex(p => p.id === followId);
  followId = alive[(at + dir + alive.length) % alive.length].id;
  followSince = replayTime;
}

// The director keeps the camera on the action: it leaves a player who has died, and after a few
// quiet seconds it moves to someone who is shooting.
function direct(f) {
  const alive = f.players.filter(p => p.alive);
  if (!alive.length) return;
  const mine = alive.find(p => p.id === followId);
  const busy = id => replayTime - (lastShotBy.get(id) ?? -1e9) < 1500;
  if (!mine) {
    const here = f.players.find(p => p.id === followId);
    alive.sort((a, b) => Math.hypot(a.x - here.x, a.y - here.y) - Math.hypot(b.x - here.x, b.y - here.y));
    followId = alive[0].id;
    followSince = replayTime;
    return;
  }
  if (!autoFollow || replayTime - followSince < 5000 || busy(followId)) return;
  const fighter = alive.find(p => p.id !== followId && busy(p.id));
  if (fighter) { followId = fighter.id; followSince = replayTime; }
}

// replayShots draws the tracers (and nearby gunfire sounds) for frames the clock has just passed.
function replayShots(upTo, now) {
  if (upTo < shotsDoneTo) shotsDoneTo = upTo; // the clock was moved back
  for (let i = shotsDoneTo + 1; i <= upTo; i++) {
    const f = R.frames[i];
    for (const s of f.shots ?? []) {
      lastShotBy.set(s.id, f.t * 1000);
      const p = f.players.find(q => q.id === s.id);
      if (!p || upTo - i > 3) continue;
      const from = new THREE.Vector3(p.x + Math.cos(s.a) * 14, 19, p.y + Math.sin(s.a) * 14);
      const to = new THREE.Vector3(p.x + Math.cos(s.a) * s.r, 19, p.y + Math.sin(s.a) * s.r);
      const line = new THREE.Line(new THREE.BufferGeometry().setFromPoints([from, to]),
        new THREE.LineBasicMaterial({ color: 0xfff0b0, transparent: true, opacity: 0.9 }));
      scene.add(line);
      tracers.push({ obj: line, until: now + 110 });
      if (!CAPTURE && camera.position.distanceTo(from) < 700) (SHOT_SOUND[p.weapon] ?? SHOT_SOUND.Pistol)();
    }
    direct(f);
  }
  shotsDoneTo = upTo;
}

// replayAdvance moves the clock and sets prev/curr for this frame. It returns the blend factor.
function replayAdvance(gameMs, now) {
  const end = (R.frames.length - 1) * R.frameMs;
  replayTime = clamp(replayTime + gameMs, 0, end);
  const i = Math.min(Math.floor(replayTime / R.frameMs), R.frames.length - 2);
  replayShots(i, now);
  prev = replaySnap(i);
  curr = replaySnap(i + 1);
  const clock = ms => `${Math.floor(ms / 60000)}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}`;
  hud.textContent = `Replay · ${curr.me.name} · ${clock(replayTime)} / ${clock(end)}${replayPaused ? ' · paused' : replaySpeed !== 1 ? ` · ${replaySpeed}x` : ''}`;
  backLink.hidden = false;
  return (replayTime - i * R.frameMs) / R.frameMs;
}

// The spectator camera trails behind and above the followed player, turning smoothly with them.
function spectatorCamera(meX, meY, dt) {
  const diff = Math.atan2(Math.sin(curr.me.aim - camYaw), Math.cos(curr.me.aim - camYaw));
  camYaw += diff * Math.min(1, dt * 2.5);
  yaw = camYaw;
  // Pull the camera in when a building would be between it and the player, and lift it as it
  // gets closer so the view stays clear.
  let want = curr.me.downed ? 60 : 78;
  const inBuilding = d => {
    const cx = meX - Math.cos(camYaw) * d, cy = meY - Math.sin(camYaw) * d;
    return curr.obstacles.some(o => o.k !== 'tree' && o.k !== 'crate'
      && cx > o.x - 8 && cx < o.x + o.w + 8 && cy > o.y - 8 && cy < o.y + o.h + 8);
  };
  for (let d = 14; d <= want; d += 8) {
    if (inBuilding(d)) { want = Math.max(10, d - 12); break; }
  }
  camBack = lerp(camBack, want, Math.min(1, dt * 5));
  const back = camBack, up = 52 + (78 - Math.min(78, camBack)) * 0.45;
  camera.position.set(meX - Math.cos(camYaw) * back, up, meY - Math.sin(camYaw) * back);
  camera.lookAt(meX + Math.cos(camYaw) * 30, 24, meY + Math.sin(camYaw) * 30);
  skyDome.position.copy(camera.position);
  for (const c of clouds.children) {
    c.position.x += dt * 6;
    if (c.position.x > 3600) c.position.x = -1600;
  }
}

window.__replay = {
  ready: false,
  duration: 0,
  // step renders one frame after moving the clock: wallMs of animation time, at the given speed.
  // Used to capture a video frame by frame.
  step(wallMs, speed = 1) {
    vclock += wallMs;
    renderFrame(vclock, wallMs / 1000, wallMs * speed);
    return { time: replayTime, done: replayTime >= (R.frames.length - 1) * R.frameMs, following: curr.me.name };
  },
  follow: id => { followId = id; autoFollow = false; },
};

if (REPLAY) {
  loadReplay();
  startBtn.hidden = true;
  if (CAPTURE) document.body.classList.add('capture'); // a clean picture for video: no buttons
  addEventListener('keydown', e => {
    if (!R) return;
    if (e.key === ' ') { e.preventDefault(); replayPaused = !replayPaused; }
    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') { autoFollow = false; switchFollow(e.key === 'ArrowRight' ? 1 : -1); }
    if (e.key === 'a' || e.key === 'A') autoFollow = true;
    if (e.key === '+' || e.key === '=') replaySpeed = Math.min(8, replaySpeed * 2);
    if (e.key === '-') replaySpeed = Math.max(0.25, replaySpeed / 2);
    if (e.key === 'r' || e.key === 'R') { replayTime = 0; shotsDoneTo = -1; lastShotBy.clear(); followSince = 0; }
  });
  for (const [btn, fn] of [
    ['rp-prev', () => { autoFollow = false; switchFollow(-1); }],
    ['rp-next', () => { autoFollow = false; switchFollow(1); }],
    ['rp-pause', () => { replayPaused = !replayPaused; }],
    ['rp-speed', () => { replaySpeed = replaySpeed >= 4 ? 1 : replaySpeed * 2; }],
    ['rp-restart', () => { replayTime = 0; shotsDoneTo = -1; lastShotBy.clear(); followSince = 0; replayPaused = false; }],
  ]) document.getElementById(btn)?.addEventListener('click', () => { if (R) fn(); });
}

requestAnimationFrame(frame);

if (new URLSearchParams(location.search).has('debug')) {
  window.__arena = {
    state: () => curr && { me: curr.me, players: curr.players, started: curr.started },
    draw: on => { renderOn = on; },
    shown: () => ({ x: shown.x, y: shown.y, predicting, pending: pendingMoves.length, ping: pingMs }),
    look: (a, p = 0) => { yaw = a; pitch = p; },
    world: () => world,
    ammo: () => curr.ammo,
    hold: (k, on) => { if (on) keys.add(k); else keys.delete(k); },
    // Low quality keeps software-rendered test browsers responsive while staging a shot.
    quality: q => { fxOn = q > 0; renderer.setPixelRatio(q > 0 ? DPR : 0.2); renderer.setSize(innerWidth, innerHeight); },
  };
}
