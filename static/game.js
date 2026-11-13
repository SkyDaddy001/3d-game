const { id, name } = window.MATCH;
const canvas = document.getElementById('game');
const ctx = canvas.getContext('2d');
const hud = document.getElementById('status');
const TICK_MS = 1000 / 30;
const lerp = (a, b, t) => a + (b - a) * t;

// Touch and mouse both go through Pointer Events. Each pointer is assigned a role:
//  - 'stick': movement joystick (bottom-left)
//  - 'fire':  fire button (bottom-right)
//  - 'aim':   anywhere else, sets the aim point
const STICK = { x: 110, y: canvas.height - 110, r: 70, hit: 90 };
const FIRE = { x: canvas.width - 110, y: canvas.height - 110, r: 55, hit: 65 };

const keys = new Set();
const mouse = { x: canvas.width / 2, y: canvas.height / 2, down: false };
const stick = { dx: 0, dy: 0 };
let fireHeld = false;
let touchActive = false;
const pointers = new Map(); // pointerId -> role

addEventListener('keydown', e => keys.add(e.key.toLowerCase()));
addEventListener('keyup', e => keys.delete(e.key.toLowerCase()));

canvas.style.touchAction = 'none'; // stop the page from scrolling while playing

function canvasPos(e) {
  const r = canvas.getBoundingClientRect();
  return {
    x: (e.clientX - r.left) * canvas.width / r.width,
    y: (e.clientY - r.top) * canvas.height / r.height,
  };
}

const dist = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);

function updateStick(p) {
  const vx = p.x - STICK.x;
  const vy = p.y - STICK.y;
  const len = Math.hypot(vx, vy);
  const k = len > STICK.r ? STICK.r / len : 1;
  stick.dx = (vx * k) / STICK.r;
  stick.dy = (vy * k) / STICK.r;
}

canvas.addEventListener('pointerdown', e => {
  canvas.setPointerCapture(e.pointerId);
  const p = canvasPos(e);
  if (inMinimap(p)) {
    const k = 2000 / MINIMAP.size;
    pendingPing = { x: (p.x - MINIMAP.x) * k, y: (p.y - MINIMAP.y) * k };
    pointers.set(e.pointerId, 'minimap');
    return;
  }
  touchActive = touchActive || e.pointerType === 'touch';
  let role;
  if (dist(p, STICK) <= STICK.hit && touchActive) {
    role = 'stick';
    updateStick(p);
  } else if (dist(p, FIRE) <= FIRE.hit && touchActive) {
    role = 'fire';
    fireHeld = true;
  } else {
    role = 'aim';
    mouse.x = p.x;
    mouse.y = p.y;
    if (e.pointerType === 'mouse') mouse.down = true;
  }
  pointers.set(e.pointerId, role);
});

canvas.addEventListener('pointermove', e => {
  const p = canvasPos(e);
  const role = pointers.get(e.pointerId);
  if (role === 'stick') updateStick(p);
  if (role === 'aim' || (!role && e.pointerType === 'mouse')) {
    mouse.x = p.x;
    mouse.y = p.y;
  }
});

function endPointer(e) {
  const role = pointers.get(e.pointerId);
  pointers.delete(e.pointerId);
  if (role === 'stick') { stick.dx = 0; stick.dy = 0; }
  if (role === 'fire') {
    fireHeld = [...pointers.values()].includes('fire');
  }
  if (role === 'aim' && e.pointerType === 'mouse') mouse.down = false;
}
canvas.addEventListener('pointerup', endPointer);
canvas.addEventListener('pointercancel', endPointer);
canvas.addEventListener('pointerleave', e => {
  if (e.pointerType === 'mouse') { mouse.down = false; }
});

const proto = location.protocol === 'https:' ? 'wss' : 'ws';
const ws = new WebSocket(`${proto}://${location.host}/ws/${id}?name=${encodeURIComponent(name)}`);

let prev = null, curr = null, currAt = 0;
let lastHitSeq = 0;
let hitFlash = { until: 0, kind: '' };
const MINIMAP = { x: 10, y: 10, size: 150 };

// Sound effects are synthesized with the Web Audio API, so no asset files are needed.
let audioCtx = null;
let muted = false;
let pendingPing = null;
let lastShotAt = 0;
let lastEventLast = '';
let prevZoneR = null;

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

addEventListener('keydown', e => {
  ensureAudio();
  const k = e.key.toLowerCase();
  if (k === 'm') { muted = !muted; return; }
  if (k === 'q' && curr && curr.me.alive) {
    const x = curr.me.x + (mouse.x - canvas.width / 2);
    const y = curr.me.y + (mouse.y - canvas.height / 2);
    pendingPing = { x: clamp(x, 0, 2000), y: clamp(y, 0, 2000) };
  }
});
addEventListener('pointerdown', ensureAudio, { once: true });

function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }

function inMinimap(p) {
  return p.x >= MINIMAP.x && p.x <= MINIMAP.x + MINIMAP.size &&
         p.y >= MINIMAP.y && p.y <= MINIMAP.y + MINIMAP.size;
}
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

  prev = curr ?? msg;
  curr = msg;
  currAt = performance.now();

  if (msg.hitSeq !== lastHitSeq) {
    lastHitSeq = msg.hitSeq;
    hitFlash = { until: performance.now() + 220, kind: msg.hitKind };
    if (msg.hitKind === 'head') SFX.headshot(); else SFX.hit();
  }
  if (!msg.started) hud.textContent = `Waiting for players (${msg.alive}), need 2+ to start`;
  else if (msg.over) hud.textContent = msg.me.place === 1 ? 'Victory Royale' : `Winner: ${msg.winner}`;
  else if (msg.me.alive) hud.textContent = `Alive: ${msg.alive} · HP ${Math.round(msg.me.hp)}`;
  else hud.textContent = `Eliminated · placed #${msg.me.place}`;
};

// Send input at the server's tick rate. Aim is relative to the screen center,
// which is where the camera keeps the player.
setInterval(() => {
  if (ws.readyState !== WebSocket.OPEN) return;
  const left = keys.has('a') || keys.has('arrowleft');
  const right = keys.has('d') || keys.has('arrowright');
  const up = keys.has('w') || keys.has('arrowup');
  const down = keys.has('s') || keys.has('arrowdown');
  const kx = (right ? 1 : 0) - (left ? 1 : 0);
  const ky = (down ? 1 : 0) - (up ? 1 : 0);
  // Keyboard and joystick are combined, then clamped to the unit square.
  const dx = Math.max(-1, Math.min(1, kx + stick.dx));
  const dy = Math.max(-1, Math.min(1, ky + stick.dy));
  const aim = Math.atan2(mouse.y - canvas.height / 2, mouse.x - canvas.width / 2);
  const shoot = mouse.down || fireHeld || keys.has(' ');
  if (shoot && curr && curr.me.alive && performance.now() - lastShotAt > 380) {
    lastShotAt = performance.now();
    SFX.shot();
  }
  let ping = false, px = 0, py = 0;
  if (pendingPing) {
    ping = true;
    px = pendingPing.x;
    py = pendingPing.y;
    pendingPing = null;
  }
  ws.send(JSON.stringify({ t: 'input', dx, dy, aim, shoot, ping, px, py }));
}, TICK_MS);

function drawPlayer(x, y, color) {
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.arc(x, y, 12, 0, Math.PI * 2);
  ctx.fill();
}

function drawHUDText(lines) {
  ctx.font = '14px sans-serif';
  ctx.textAlign = 'right';
  ctx.fillStyle = '#fff';
  lines.forEach((line, i) => ctx.fillText(line, canvas.width - 10, 20 + i * 18));
}

// On-screen controls. Drawn only after the first touch so desktop stays clean.
function drawTouchControls() {
  ctx.save();
  ctx.globalAlpha = 0.35;
  ctx.fillStyle = '#000';
  ctx.beginPath();
  ctx.arc(STICK.x, STICK.y, STICK.r, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = '#fff';
  ctx.beginPath();
  ctx.arc(STICK.x + stick.dx * STICK.r, STICK.y + stick.dy * STICK.r, 26, 0, Math.PI * 2);
  ctx.fill();

  ctx.globalAlpha = fireHeld ? 0.7 : 0.35;
  ctx.fillStyle = '#ef5350';
  ctx.beginPath();
  ctx.arc(FIRE.x, FIRE.y, FIRE.r, 0, Math.PI * 2);
  ctx.fill();
  ctx.globalAlpha = 1;
  ctx.fillStyle = '#fff';
  ctx.font = 'bold 16px sans-serif';
  ctx.textAlign = 'center';
  ctx.fillText('FIRE', FIRE.x, FIRE.y + 6);
  ctx.restore();
}

// Crosshair ticks spread outward as bloom grows, like a real weapon's accuracy ring.
function drawCrosshair(bloom) {
  const gap = 6 + bloom * 200;
  const cx = canvas.width / 2, cy = canvas.height / 2;
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
  const cx = canvas.width / 2, cy = canvas.height / 2;
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

// Minimap: zone, obstacles, the local player, and anyone currently revealed by a bounty.
function drawMinimap(curr) {
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

  const pulse = 0.5 + 0.5 * Math.sin(performance.now() / 150);
  for (const b of curr.revealed) {
    ctx.fillStyle = `rgba(255, 82, 82, ${0.5 + 0.5 * pulse})`;
    ctx.beginPath();
    ctx.arc(x + b.x * k, y + b.y * k, 4, 0, Math.PI * 2);
    ctx.fill();
  }

  for (const pg of curr.pings) {
    ctx.fillStyle = '#ffeb3b';
    ctx.beginPath();
    ctx.arc(x + pg.x * k, y + pg.y * k, 3, 0, Math.PI * 2);
    ctx.fill();
  }
  for (const mate of curr.players) {
    if (mate.team === 0 || mate.team !== curr.me.team) continue;
    ctx.fillStyle = '#66bb6a';
    ctx.beginPath();
    ctx.arc(x + mate.x * k, y + mate.y * k, 3.5, 0, Math.PI * 2);
    ctx.fill();
  }
  if (curr.me.alive) {
    ctx.fillStyle = '#4fc3f7';
    ctx.beginPath();
    ctx.arc(x + curr.me.x * k, y + curr.me.y * k, 4, 0, Math.PI * 2);
    ctx.fill();
  }
  ctx.restore();
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.4)';
  ctx.strokeRect(x, y, size, size);
}

function render(now) {
  requestAnimationFrame(render);
  ctx.fillStyle = '#1b2a1b';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  if (!curr) return;

  const t = Math.min(1, (now - currAt) / TICK_MS);
  const meX = lerp(prev.me.x, curr.me.x, t);
  const meY = lerp(prev.me.y, curr.me.y, t);

  ctx.save();
  ctx.translate(canvas.width / 2 - meX, canvas.height / 2 - meY);

  ctx.strokeStyle = '#444';
  ctx.lineWidth = 4;
  ctx.strokeRect(0, 0, 2000, 2000);

  ctx.fillStyle = '#5b5b5b';
  for (const r of curr.obstacles) ctx.fillRect(r.x, r.y, r.w, r.h);

  ctx.fillStyle = '#f5c542';
  for (const l of curr.loot) ctx.fillRect(l.x - 5, l.y - 5, 10, 10);

  // Darken everything outside the zone: a big rect with a circular hole (evenodd).
  const z = curr.zone;
  ctx.fillStyle = 'rgba(180, 0, 0, 0.25)';
  ctx.beginPath();
  ctx.rect(-5000, -5000, 10000, 10000);
  ctx.moveTo(z.x + z.r, z.y);
  ctx.arc(z.x, z.y, z.r, 0, Math.PI * 2);
  ctx.fill('evenodd');

  const prevById = new Map(prev.players.map(p => [p.id, p]));
  for (const p of curr.players) {
    const old = prevById.get(p.id) ?? p;
    const mate = curr.me.team !== 0 && p.team === curr.me.team;
    drawPlayer(lerp(old.x, p.x, t), lerp(old.y, p.y, t), mate ? '#66bb6a' : '#ef5350');
  }
  for (const b of curr.revealed) {
    ctx.strokeStyle = '#ff5252';
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.arc(b.x, b.y, 22 + 4 * Math.sin(now / 150), 0, Math.PI * 2);
    ctx.stroke();
    ctx.fillStyle = '#ff5252';
    ctx.font = 'bold 12px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText('BOUNTY', b.x, b.y - 28);
  }
  for (const pg of curr.pings) {
    ctx.strokeStyle = '#ffeb3b';
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.arc(pg.x, pg.y, 14 + 6 * Math.sin(now / 120), 0, Math.PI * 2);
    ctx.stroke();
    ctx.fillStyle = '#ffeb3b';
    ctx.font = '11px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText(`${pg.name} · PING`, pg.x, pg.y - 22);
  }
  drawPlayer(meX, meY, curr.me.alive ? '#4fc3f7' : '#607d8b');

  const aim = Math.atan2(mouse.y - canvas.height / 2, mouse.x - canvas.width / 2);
  ctx.strokeStyle = '#fff';
  ctx.beginPath();
  ctx.moveTo(meX, meY);
  ctx.lineTo(meX + Math.cos(aim) * 40, meY + Math.sin(aim) * 40);
  ctx.stroke();
  ctx.restore();

  drawHUDText([`Alive: ${curr.alive}`, ...curr.events]);
  if (curr.me.alive) {
    drawCrosshair(curr.me.bloom);
    drawHitMarker(now);
  }
  drawMinimap(curr);
  if (touchActive) drawTouchControls();

  ctx.textAlign = 'center';
  if (curr.started && !curr.me.alive && !curr.over) {
    ctx.fillStyle = '#fff';
    ctx.font = '32px sans-serif';
    ctx.fillText(`Eliminated — #${curr.me.place}`, canvas.width / 2, canvas.height / 2);
  }
  if (curr.over) {
    ctx.fillStyle = 'rgba(0, 0, 0, 0.55)';
    ctx.fillRect(0, canvas.height / 2 - 60, canvas.width, 120);
    ctx.fillStyle = '#ffd54f';
    ctx.font = 'bold 36px sans-serif';
    const title = curr.me.place === 1 ? 'Victory Royale' : `${curr.winner} wins`;
    ctx.fillText(title, canvas.width / 2, canvas.height / 2);
    ctx.fillStyle = '#fff';
    ctx.font = '18px sans-serif';
    ctx.fillText(`You placed #${curr.me.place}`, canvas.width / 2, canvas.height / 2 + 30);
  }
}
requestAnimationFrame(render);
